package notify

import (
	"context"
	"testing"
)

// fakeSender records the payload it was asked to send.
type fakeSender struct {
	called  bool
	payload map[string]interface{}
}

func (f *fakeSender) Send(_ context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	f.called = true
	f.payload = payload
	return map[string]interface{}{"ok": true}, nil
}

// fakeReactor implements both Sender and Reactor.
type fakeReactor struct {
	fakeSender
	reacted bool
	payload map[string]interface{}
}

func (f *fakeReactor) React(_ context.Context, payload map[string]interface{}) (map[string]interface{}, error) {
	f.reacted = true
	f.payload = payload
	return map[string]interface{}{"ok": true}, nil
}

func TestHandleDoCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("no command defaults to send", func(t *testing.T) {
		f := &fakeSender{}
		got, err := HandleDoCommand(ctx, f, map[string]interface{}{"text": "hi"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.called {
			t.Fatal("expected Send to be called")
		}
		if got["ok"] != true {
			t.Fatalf("expected ok=true, got %v", got)
		}
		if f.payload["text"] != "hi" {
			t.Fatalf("payload not forwarded, got %v", f.payload)
		}
	})

	t.Run("explicit send command", func(t *testing.T) {
		f := &fakeSender{}
		if _, err := HandleDoCommand(ctx, f, map[string]interface{}{"command": "send"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.called {
			t.Fatal("expected Send to be called")
		}
	})

	t.Run("unknown command errors", func(t *testing.T) {
		f := &fakeSender{}
		_, err := HandleDoCommand(ctx, f, map[string]interface{}{"command": "explode"})
		if err == nil {
			t.Fatal("expected an error for unknown command")
		}
		if f.called {
			t.Fatal("Send should not be called for unknown command")
		}
	})

	t.Run("react routes to Reactor", func(t *testing.T) {
		f := &fakeReactor{}
		got, err := HandleDoCommand(ctx, f, map[string]interface{}{"command": "react", "name": "white_check_mark"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.reacted {
			t.Fatal("expected React to be called")
		}
		if got["ok"] != true {
			t.Fatalf("expected ok=true, got %v", got)
		}
		if f.payload["name"] != "white_check_mark" {
			t.Fatalf("payload not forwarded, got %v", f.payload)
		}
	})

	t.Run("react on a non-Reactor errors", func(t *testing.T) {
		f := &fakeSender{}
		if _, err := HandleDoCommand(ctx, f, map[string]interface{}{"command": "react"}); err == nil {
			t.Fatal("expected an error when backend does not support react")
		}
	})
}
