package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.viam.com/rdk/logging"
)

func testLogger() logging.Logger { return logging.NewLogger("test") }

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		cfg     Config
		wantErr bool
	}{
		"no credentials":      {Config{}, true},
		"both credentials":    {Config{BotToken: "xoxb-1", WebhookURL: "https://h"}, true},
		"bot token only":      {Config{BotToken: "xoxb-1"}, false},
		"webhook only":        {Config{WebhookURL: "https://h"}, false},
		"bot with default ch": {Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := tc.cfg.Validate("services.0")
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestSendRequiresTextOrBlocks(t *testing.T) {
	s := New(&Config{BotToken: "xoxb-1"}, testLogger())
	if _, err := s.Send(context.Background(), map[string]interface{}{}); err == nil {
		t.Fatal("expected error when neither text nor blocks is provided")
	}
}

func TestSendBotMessage(t *testing.T) {
	var gotAuth string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"ts":"123.456","channel":"C42"}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-secret"}, testLogger())
	s.postURL = srv.URL

	res, err := s.Send(context.Background(), map[string]interface{}{
		"channel_id": "C0ALERTS",
		"text":       "hello",
		"thread_ts":  "111.222",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer xoxb-secret" {
		t.Fatalf("missing/wrong auth header: %q", gotAuth)
	}
	if gotBody["channel"] != "C0ALERTS" || gotBody["text"] != "hello" || gotBody["thread_ts"] != "111.222" {
		t.Fatalf("request body not as expected: %v", gotBody)
	}
	if res["ok"] != true || res["ts"] != "123.456" || res["channel"] != "C42" {
		t.Fatalf("unexpected result: %v", res)
	}
}

func TestSendBotMessageUsesDefaultChannel(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1","channel":"C1"}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0DEFAULT"}, testLogger())
	s.postURL = srv.URL

	if _, err := s.Send(context.Background(), map[string]interface{}{"text": "hi"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["channel"] != "C0DEFAULT" {
		t.Fatalf("expected default channel to be used, got %v", gotBody["channel"])
	}
}

func TestSendBotMessageMissingChannel(t *testing.T) {
	s := newSlackResource(&Config{BotToken: "xoxb-1"}, testLogger())
	s.postURL = "http://unused.invalid"
	_, err := s.Send(context.Background(), map[string]interface{}{"text": "hi"})
	if err == nil {
		t.Fatal("expected error when no channel_id and no default_channel_id")
	}
}

func TestSendBotMessageSlackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slack returns HTTP 200 with ok=false on logical failures.
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0NOPE"}, testLogger())
	s.postURL = srv.URL

	_, err := s.Send(context.Background(), map[string]interface{}{"text": "hi"})
	if err == nil {
		t.Fatal("expected error when Slack returns ok=false")
	}
}

func TestReact(t *testing.T) {
	var gotAuth string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-secret"}, testLogger())
	s.reactURL = srv.URL

	res, err := s.React(context.Background(), map[string]interface{}{
		"channel": "C42",
		"ts":      "123.456",
		"name":    "white_check_mark",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer xoxb-secret" {
		t.Fatalf("missing/wrong auth header: %q", gotAuth)
	}
	if gotBody["channel"] != "C42" || gotBody["timestamp"] != "123.456" || gotBody["name"] != "white_check_mark" {
		t.Fatalf("request body not as expected: %v", gotBody)
	}
	if res["ok"] != true {
		t.Fatalf("unexpected result: %v", res)
	}
}

// TestReactAcceptsSendResult locks the convention that React accepts the map
// Send returns verbatim (plus a "name"), so a caller can echo it back without
// interpreting the message identity.
func TestReactAcceptsSendResult(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1"}, testLogger())
	s.reactURL = srv.URL

	// Exactly what sendBotMessage returns, with a reaction name added.
	sendResult := map[string]interface{}{"ok": true, "ts": "123.456", "channel": "C42"}
	sendResult["name"] = "white_check_mark"
	if _, err := s.React(context.Background(), sendResult); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["channel"] != "C42" || gotBody["timestamp"] != "123.456" {
		t.Fatalf("send result not honored: %v", gotBody)
	}
}

func TestReactUsesDefaultChannel(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0DEFAULT"}, testLogger())
	s.reactURL = srv.URL

	if _, err := s.React(context.Background(), map[string]interface{}{
		"ts": "1.2", "name": "x",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["channel"] != "C0DEFAULT" {
		t.Fatalf("expected default channel to be used, got %v", gotBody["channel"])
	}
}

func TestReactAlreadyReactedIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"already_reacted"}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C1"}, testLogger())
	s.reactURL = srv.URL

	res, err := s.React(context.Background(), map[string]interface{}{"ts": "1.2", "name": "x"})
	if err != nil {
		t.Fatalf("already_reacted should be treated as success, got %v", err)
	}
	if res["ok"] != true {
		t.Fatalf("expected ok=true, got %v", res)
	}
}

func TestReactSlackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"message_not_found"}`))
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C1"}, testLogger())
	s.reactURL = srv.URL

	if _, err := s.React(context.Background(), map[string]interface{}{"ts": "1.2", "name": "x"}); err == nil {
		t.Fatal("expected error when Slack returns a real error")
	}
}

func TestReactRequiresFields(t *testing.T) {
	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C1"}, testLogger())
	s.reactURL = "http://unused.invalid"
	if _, err := s.React(context.Background(), map[string]interface{}{"ts": "1.2"}); err == nil {
		t.Fatal("expected error when name is missing")
	}
	if _, err := s.React(context.Background(), map[string]interface{}{"name": "x"}); err == nil {
		t.Fatal("expected error when ts is missing")
	}
}

func TestReactRequiresBotToken(t *testing.T) {
	s := newSlackResource(&Config{WebhookURL: "https://h"}, testLogger())
	if _, err := s.React(context.Background(), map[string]interface{}{"ts": "1.2", "name": "x"}); err == nil {
		t.Fatal("expected error: webhook notifiers cannot react")
	}
}

func TestSendWebhook(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	s := New(&Config{WebhookURL: srv.URL}, testLogger())

	res, err := s.Send(context.Background(), map[string]interface{}{"text": "via webhook"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["text"] != "via webhook" {
		t.Fatalf("webhook body not as expected: %v", gotBody)
	}
	if res["ok"] != true {
		t.Fatalf("expected ok=true, got %v", res)
	}
}
