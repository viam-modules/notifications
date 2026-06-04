# Module notifications

A generic **notifications** module for Viam. Each model is a generic *service*
that accepts a free-form payload via `DoCommand` and delivers it to some
destination. The module is built to grow: the Slack model ships today, and
email / SMS / other backends can be added as additional models without changing
the calling convention.

## How it works

All models share one contract (see [`notify/notify.go`](notify/notify.go)):

- `DoCommand` accepts an optional `"command"` key (defaults to `"send"`).
- The remaining keys are the message payload, interpreted by each backend.
- On success a non-nil result map is returned (at minimum `{"ok": true}`).

```
cmd/module/main.go        module entrypoint — registers every model
notify/notify.go          shared Sender interface + DoCommand dispatcher
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

(`ts` and `channel` are populated for the bot token path; the webhook path
returns just `{ "ok": true }`.)

---

## Adding a new model

The generic design means a new backend (email, SMS, etc.) is a small, isolated
addition:

1. Create `models/<name>/<name>.go` with a `Config`, a `New` constructor, and an
   `init()` that calls `resource.RegisterService(generic.API, Model, ...)`.
2. Implement the `notify.Sender` interface (`Send(ctx, payload)`), and have
   `DoCommand` delegate to `notify.HandleDoCommand`.
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
