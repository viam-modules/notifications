# Module notifications

A generic **notifications** module for Viam. Each model is a generic *service*
that accepts a free-form payload via `DoCommand` and delivers it to some
destination. The module is built to grow: the Slack model ships today, and
email / SMS / other backends can be added as additional models without changing
the calling convention.

## How it works

All models share one contract (see [`notify/notify.go`](notify/notify.go)):

- `DoCommand` accepts an optional `"command"` key (defaults to `"send"`).
  `"react"` adds an emoji reaction to a previously-sent message, and `"poll"`
  reads messages back, on backends that support them.
- The remaining keys are the message payload, interpreted by each backend.
- On success a non-nil result map is returned (at minimum `{"ok": true}`).

```
cmd/module/main.go        module entrypoint — registers every model
notify/notify.go          shared Sender/Reactor/Reader interfaces + DoCommand dispatcher
models/slack/slack.go     viam:notifications:slack
models/<name>/<name>.go   future backends (email, sms, ...)
```

## Models

This module provides the following model(s):

- [`viam:notifications:slack`](#model-viamnotificationsslack) — post a message to a Slack channel.

---

## Model `viam:notifications:slack`

A generic service that posts a message to Slack. It supports two delivery modes,
selected by which credential you configure:

- **Bot token** (`bot_token`) — posts via Slack's `chat.postMessage` API. Lets
  each `DoCommand` choose the channel and reply in threads.
- **Incoming webhook** (`webhook_url`) — simpler, but the destination channel is
  fixed by the webhook.

Configure exactly one of `bot_token` or `webhook_url`.

### Configuration

```json
{
  "bot_token": "xoxb-your-slack-bot-token",
  "default_channel_id": "C0123456789"
}
```

or, using an incoming webhook:

```json
{
  "webhook_url": "https://hooks.slack.com/services/T000/B000/XXXX"
}
```

#### Attributes

| Name              | Type   | Inclusion                  | Description                                                                 |
|-------------------|--------|----------------------------|-----------------------------------------------------------------------------|
| `bot_token`       | string | Required (one credential)  | Slack bot OAuth token (`xoxb-...`). Posts via `chat.postMessage`.            |
| `webhook_url`     | string | Required (one credential)  | Slack incoming webhook URL. Channel is fixed by the webhook.                 |
| `default_channel_id` | string | Optional                | Channel ID used when a `DoCommand` omits `channel_id` (bot token only).      |

### DoCommand

Send a message by calling `DoCommand`. The optional `command` key defaults to
`"send"`, so it can be omitted.

#### Payload keys

| Key         | Type   | Description                                                                 |
|-------------|--------|-----------------------------------------------------------------------------|
| `text`       | string | Message text. Required unless `blocks` is provided.                          |
| `blocks`     | array  | Slack [Block Kit](https://api.slack.com/block-kit) blocks, passed through.   |
| `channel_id` | string | Slack channel ID. Overrides `default_channel_id` (bot token only).          |
| `thread_ts`  | string | Timestamp of a parent message to reply in-thread (bot token only).          |

#### Example DoCommand

Simple text message:

```json
{
  "channel_id": "C0123456789",
  "text": "Deployment finished successfully :white_check_mark:"
}
```

Block Kit message in a thread:

```json
{
  "channel_id": "C0123456789",
  "thread_ts": "1700000000.000100",
  "text": "Build failed",
  "blocks": [
    {
      "type": "section",
      "text": { "type": "mrkdwn", "text": "*Build failed* on `main`" }
    }
  ]
}
```

#### Result

On success the command returns:

```json
{ "ok": true, "ts": "1700000000.000200", "channel": "C0123456789" }
```

(`ts`, `thread_ts` and `channel` are populated for the bot token path; the
webhook path returns just `{ "ok": true }`.)

`ts` identifies **this message** and `thread_ts` identifies **the conversation
it is in**. They are equal when the message opened the thread, because a Slack
thread borrows its root message's timestamp as its id. Keep `thread_ts` to
continue or read the conversation, and `ts` to react to that one message — a
`send` result can be handed straight to either `react` or `poll`.

#### Adding a reaction (`command: "react"`)

Add an emoji reaction to an existing message with `{"command": "react", ...}`.
This requires a **bot token** (the webhook path cannot react). The message is
identified by the same keys `send` returns (`ts` and `channel`), so you can hand
the `send` result straight back with a `name` added.

| Key       | Type   | Description                                                                 |
|-----------|--------|-----------------------------------------------------------------------------|
| `name`     | string | Emoji name **without** colons, e.g. `white_check_mark`. Required.            |
| `ts`       | string | The target message's timestamp, as returned by `send`. Required.            |
| `channel`  | string | Channel ID the message is in. Defaults to `default_channel_id`.              |

```json
{
  "command": "react",
  "channel": "C0123456789",
  "ts": "1700000000.000200",
  "name": "white_check_mark"
}
```

Returns `{ "ok": true }`. An `already_reacted` response from Slack is treated as
success, so re-issuing the same reaction is idempotent.

#### Reading a thread (`command: "poll"`)

Read replies in a thread with `{"command": "poll", ...}`, so a caller can follow
a conversation instead of only broadcasting into it. This requires a **bot
token** (the webhook path cannot read) and the `channels:history` scope
(`groups:history` for a private channel), plus `files:read` to relay
attachments.

| Key          | Type   | Description                                                                 |
|--------------|--------|-----------------------------------------------------------------------------|
| `thread_ts`  | string | The thread to read, as returned by `send`. Required.                        |
| `channel_id` | string | Channel the thread is in; `channel` is also accepted. Defaults to `default_channel_id`. |
| `since_ts`   | string | Cursor. Only messages strictly newer than this are returned.                |
| `include_images` | bool | Relay image attachments as data URIs. Off by default; needs `files:read`. |

```json
{
  "command": "poll",
  "thread_ts": "1700000000.000100",
  "since_ts": "1700000000.000200"
}
```

Since `send` returns both `thread_ts` and `channel`, its result is already a
valid `poll` payload — add `since_ts` as you go.

Returns the replies oldest-first:

```json
{
  "ok": true,
  "messages": [
    { "ts": "1700000000.000300", "text": "on it", "user": "U0123456789" }
  ]
}
```

With `include_images`, each message also carries an `images` list of JPEG data
URIs, plus `image_errors` naming any attachment that could not be fetched — an
unreachable attachment never drops the message it came with. Slack serves
attachments from `url_private`, which needs the bot token in a header, so a
browser cannot fetch them itself; images are downscaled to 1280px and re-encoded
as JPEG so a caller on a constrained link is not handed a multi-megabyte
screenshot.

Two behaviours worth knowing. **The caller's own messages are not returned** —
everything this service posts is posted by the bot, so echoing them back would
double what the caller already has; keep your own sends locally and merge by
`ts`. And **`since_ts` is a cursor, not a filter on your side**: pass back the
newest `ts` you have seen and Slack is asked to skip the rest, so a long thread
does not re-transfer its history on every poll.

---

## Adding a new model

The generic design means a new backend (email, SMS, etc.) is a small, isolated
addition:

1. Create `models/<name>/<name>.go` with a `Config`, a `New` constructor, and an
   `init()` that calls `resource.RegisterService(generic.API, Model, ...)`.
2. Implement the `notify.Sender` interface (`Send(ctx, payload)`), and have
   `DoCommand` delegate to `notify.HandleDoCommand`. Optionally implement
   `notify.Reactor` (`React(ctx, payload)`) to support the `"react"` command and
   `notify.Reader` (`Poll(ctx, payload)`) to support `"poll"`; backends that
   don't report those commands as unsupported.
3. Register the model in [`cmd/module/main.go`](cmd/module/main.go) by adding one
   `resource.APIModel{API: generic.API, Model: <name>.Model}` line.

The `notify.HandleDoCommand` dispatcher and the `"send"` command convention are
shared automatically, so callers use every backend the same way.

## Development

```bash
make setup     # go mod tidy
make           # build bin/notifications
make test      # go test ./...
make lint      # gofmt -s -w .
make module    # test + build module.tar.gz for upload
```

The module is registered at https://app.viam.com/module/viam/notifications and
deployed via the GitHub Actions workflow in
[`.github/workflows/deploy.yml`](.github/workflows/deploy.yml) on tagged releases.
