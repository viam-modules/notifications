package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPollRequiresBotToken(t *testing.T) {
	s := newSlackResource(&Config{WebhookURL: "https://hooks.example"}, testLogger())
	if _, err := s.Poll(context.Background(), map[string]interface{}{"thread_ts": "111.000"}); err == nil {
		t.Fatal("expected an error: a webhook cannot read a thread")
	}
}

func TestPollRequiresThreadTS(t *testing.T) {
	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = "http://unused.invalid"
	if _, err := s.Poll(context.Background(), map[string]interface{}{}); err == nil {
		t.Fatal("expected an error when thread_ts is missing")
	}
}

func TestPollRequiresChannel(t *testing.T) {
	s := newSlackResource(&Config{BotToken: "xoxb-1"}, testLogger())
	s.repliesURL = "http://unused.invalid"
	_, err := s.Poll(context.Background(), map[string]interface{}{"thread_ts": "111.000"})
	if err == nil {
		t.Fatal("expected an error with no channel_id and no default_channel_id")
	}
}

func TestPollDropsRootAndBotEcho(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-secret" {
			t.Errorf("missing bot token, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "messages": []interface{}{
			map[string]interface{}{"ts": "111.000", "text": "root", "bot_id": "B1"},
			map[string]interface{}{"ts": "112.000", "text": "our own send", "bot_id": "B1"},
			map[string]interface{}{"ts": "113.000", "text": "a human reply", "user": "U9"},
		}})
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-secret", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = srv.URL

	res, err := s.Poll(context.Background(), map[string]interface{}{"thread_ts": "111.000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msgs, ok := res["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected a messages list, got %v", res["messages"])
	}
	// The root and anything the bot posted are the caller's own words coming
	// back; only the human reply is new information.
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %v", len(msgs), msgs)
	}
	if got := msgs[0].(map[string]interface{})["text"]; got != "a human reply" {
		t.Fatalf("unexpected message %v", got)
	}
	if !strings.Contains(gotQuery, "ts=111.000") || !strings.Contains(gotQuery, "channel=C0A") {
		t.Errorf("thread and channel should reach Slack as query params, got %q", gotQuery)
	}
}

func TestPollForwardsCursorAndReChecksBoundary(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		// Slack treats "oldest" as inclusive, so it hands back the very message
		// the caller already has.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "messages": []interface{}{
			map[string]interface{}{"ts": "113.000", "text": "already seen", "user": "U9"},
			map[string]interface{}{"ts": "114.000", "text": "new", "user": "U9"},
		}})
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = srv.URL

	res, err := s.Poll(context.Background(), map[string]interface{}{
		"thread_ts": "111.000", "since_ts": "113.000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotQuery, "oldest=113.000") {
		t.Errorf("cursor should reach Slack as oldest, got %q", gotQuery)
	}
	msgs := res["messages"].([]interface{})
	if len(msgs) != 1 || msgs[0].(map[string]interface{})["text"] != "new" {
		t.Fatalf("expected the boundary message filtered out, got %v", msgs)
	}
}

func TestPollSurfacesSlackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// HTTP 200 with ok=false; trusting the status code would report an
		// unreadable channel as an empty thread.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "not_in_channel"})
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = srv.URL

	if _, err := s.Poll(context.Background(), map[string]interface{}{"thread_ts": "111.000"}); err == nil {
		t.Fatal("expected not_in_channel to surface as an error")
	}
}

func TestTSAfter(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		// Parsed as float64 these lose the counter and compare equal.
		{"1700000000.000200", "1700000000.000100", true},
		{"1700000000.000100", "1700000000.000200", false},
		{"1700000001.000000", "1700000000.999999", true},
		{"1700000000.000100", "1700000000.000100", false},
	}
	for _, tc := range cases {
		if got := tsAfter(tc.a, tc.b); got != tc.want {
			t.Errorf("tsAfter(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSendReturnsTheThreadItWroteInto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ts := "111.000"
		if _, threaded := body["thread_ts"]; threaded {
			ts = "999.000"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "ts": ts, "channel": "C0A",
		})
	}))
	defer srv.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, testLogger())
	s.postURL = srv.URL
	ctx := context.Background()

	// Opening a thread: the message's own ts is the thread id, so they match.
	opened, err := s.Send(ctx, map[string]interface{}{"text": "help"})
	if err != nil {
		t.Fatal(err)
	}
	if opened["ts"] != "111.000" || opened["thread_ts"] != "111.000" {
		t.Fatalf("opening a thread should return ts == thread_ts, got %v", opened)
	}

	// Replying in one: ts is this message, thread_ts is the conversation.
	replied, err := s.Send(ctx, map[string]interface{}{"text": "more", "thread_ts": "111.000"})
	if err != nil {
		t.Fatal(err)
	}
	if replied["ts"] != "999.000" {
		t.Errorf("expected the reply's own ts, got %v", replied["ts"])
	}
	if replied["thread_ts"] != "111.000" {
		t.Errorf("expected the thread it was posted into, got %v", replied["thread_ts"])
	}
}

// TestPollAcceptsSendResult locks the convention that a Send result can be
// handed to Poll unchanged, the same way React already accepts one.
func TestPollAcceptsSendResult(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "messages": []interface{}{}})
	}))
	defer srv.Close()

	// No default_channel_id: the channel has to come from the Send result.
	s := newSlackResource(&Config{BotToken: "xoxb-1"}, testLogger())
	s.repliesURL = srv.URL

	sendResult := map[string]interface{}{
		"ok": true, "ts": "111.000", "thread_ts": "111.000", "channel": "C0FROMSEND",
	}
	if _, err := s.Poll(context.Background(), sendResult); err != nil {
		t.Fatalf("a Send result should be a valid Poll payload: %v", err)
	}
	if !strings.Contains(gotQuery, "channel=C0FROMSEND") {
		t.Errorf(`Poll should accept "channel" as Send returns it, got %q`, gotQuery)
	}
	if !strings.Contains(gotQuery, "ts=111.000") {
		t.Errorf("expected the thread from the Send result, got %q", gotQuery)
	}
}
