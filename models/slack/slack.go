// Package slack implements the viam:notifications:slack model, a generic
// service that posts messages to a Slack channel.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"

	"notifications/notify"
)

// Model is the Viam model triplet for the Slack notifier.
var Model = resource.NewModel("viam", "notifications", "slack")

const postMessageURL = "https://slack.com/api/chat.postMessage"

func init() {
	resource.RegisterService(generic.API, Model, resource.Registration[resource.Resource, *Config]{
		Constructor: newSlack,
	})
}

// Config is the configuration for a Slack notifier. Exactly one credential must
// be supplied.
type Config struct {
	// BotToken is a Slack bot OAuth token (xoxb-...). When set, messages are
	// posted via chat.postMessage, which allows per-message channel selection
	// and threading.
	BotToken string `json:"bot_token,omitempty"`
	// WebhookURL is a Slack incoming webhook URL. Simpler than a bot token, but
	// the destination channel is fixed by the webhook itself.
	WebhookURL string `json:"webhook_url,omitempty"`
	// DefaultChannel is used when a DoCommand does not specify "channel". Only
	// applies to the bot token path.
	DefaultChannel string `json:"default_channel,omitempty"`
}

// Validate ensures exactly one credential is configured.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	switch {
	case cfg.BotToken == "" && cfg.WebhookURL == "":
		return nil, nil, fmt.Errorf(`%s: one of "bot_token" or "webhook_url" is required`, path)
	case cfg.BotToken != "" && cfg.WebhookURL != "":
		return nil, nil, fmt.Errorf(`%s: set only one of "bot_token" or "webhook_url"`, path)
	}
	return nil, nil, nil
}

type slack struct {
	resource.AlwaysRebuild
	resource.Named

	logger logging.Logger
	cfg    *Config
	client *http.Client

	// postURL is the chat.postMessage endpoint. It is a field (rather than the
	// package const directly) so tests can point it at a mock server.
	postURL string
}

func newSlack(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	s := New(conf, logger)
	s.Named = rawConf.ResourceName().AsNamed()
	return s, nil
}

// New constructs a Slack notifier directly from a Config. It is used both by the
// Viam constructor and by callers (such as the cli harness) that want to drive
// the Sender without a running machine.
func New(cfg *Config, logger logging.Logger) *slack {
	return &slack{
		logger:  logger,
		cfg:     cfg,
		client:  &http.Client{Timeout: 15 * time.Second},
		postURL: postMessageURL,
	}
}

// DoCommand is the entrypoint for triggering a notification. See Send for the
// recognized payload keys.
func (s *slack) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return notify.HandleDoCommand(ctx, s, cmd)
}

// Send posts a message to Slack. Recognized payload keys:
//   - "channel"   (string) channel id or name; defaults to default_channel (bot token only)
//   - "text"      (string) message text
//   - "blocks"    (array)  Slack Block Kit blocks, passed through as-is
//   - "thread_ts" (string) reply within a thread (bot token only)
//
// Either "text" or "blocks" must be present.
func (s *slack) Send(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	text, _ := payload["text"].(string)
	blocks := payload["blocks"]
	if text == "" && blocks == nil {
		return nil, errors.New(`slack: "text" or "blocks" is required`)
	}

	if s.cfg.WebhookURL != "" {
		return s.sendWebhook(ctx, text, blocks)
	}
	return s.sendBotMessage(ctx, payload, text, blocks)
}

func (s *slack) sendBotMessage(ctx context.Context, payload map[string]interface{}, text string, blocks interface{}) (map[string]interface{}, error) {
	channel, _ := payload["channel"].(string)
	if channel == "" {
		channel = s.cfg.DefaultChannel
	}
	if channel == "" {
		return nil, errors.New(`slack: "channel" is required (no default_channel configured)`)
	}

	body := map[string]interface{}{"channel": channel}
	if text != "" {
		body["text"] = text
	}
	if blocks != nil {
		body["blocks"] = blocks
	}
	if ts, ok := payload["thread_ts"].(string); ok && ts != "" {
		body["thread_ts"] = ts
	}

	raw, err := s.post(ctx, s.postURL, body, map[string]string{
		"Authorization": "Bearer " + s.cfg.BotToken,
	})
	if err != nil {
		return nil, err
	}

	// chat.postMessage returns HTTP 200 even on logical failures, with ok=false.
	var parsed struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		TS      string `json:"ts"`
		Channel string `json:"channel"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("slack: decoding response: %w", err)
	}
	if !parsed.OK {
		return nil, fmt.Errorf("slack: chat.postMessage failed: %s", parsed.Error)
	}
	return map[string]interface{}{"ok": true, "ts": parsed.TS, "channel": parsed.Channel}, nil
}

func (s *slack) sendWebhook(ctx context.Context, text string, blocks interface{}) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if text != "" {
		body["text"] = text
	}
	if blocks != nil {
		body["blocks"] = blocks
	}
	if _, err := s.post(ctx, s.cfg.WebhookURL, body, nil); err != nil {
		return nil, err
	}
	return map[string]interface{}{"ok": true}, nil
}

func (s *slack) post(ctx context.Context, url string, body map[string]interface{}, headers map[string]string) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("slack: encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("slack: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("slack: http %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (s *slack) Close(context.Context) error {
	return nil
}
