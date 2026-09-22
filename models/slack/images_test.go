package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShrinkToJPEGBoundsAndReencodes(t *testing.T) {
	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 3000, 1500))
	src.Set(10, 10, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	shrunk, err := shrinkToJPEG(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(shrunk))
	if err != nil {
		t.Fatalf("result is not a decodable image: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("expected jpeg, got %s", format)
	}
	if got := decoded.Bounds().Dx(); got != maxImageDimension {
		t.Errorf("expected width bounded to %d, got %d", maxImageDimension, got)
	}
	if got := decoded.Bounds().Dy(); got != 640 {
		t.Errorf("expected 3000x1500 to become 1280x640, got height %d", got)
	}
	if len(shrunk) >= buf.Len() {
		t.Errorf("expected a smaller image: %d >= %d", len(shrunk), buf.Len())
	}
}

func TestShrinkToJPEGRejectsGarbage(t *testing.T) {
	if _, err := shrinkToJPEG([]byte("not an image")); err == nil {
		t.Fatal("expected an error decoding garbage")
	}
}

func TestPollRelaysImagesOnlyWhenAsked(t *testing.T) {
	var files *httptest.Server
	files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xoxb-secret" {
			t.Errorf("attachment fetch must carry the bot token, got %q", got)
		}
		var buf bytes.Buffer
		_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
		_, _ = w.Write(buf.Bytes())
	}))
	defer files.Close()

	replies := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "messages": []interface{}{
			map[string]interface{}{"ts": "113.000", "text": "see this", "user": "U9",
				"files": []interface{}{map[string]interface{}{
					"mimetype": "image/png", "url_private": files.URL + "/shot.png",
				}}},
		}})
	}))
	defer replies.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-secret", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = replies.URL

	// Without the flag, attachments are not fetched at all.
	res, err := s.Poll(context.Background(), map[string]interface{}{"thread_ts": "111.000"})
	if err != nil {
		t.Fatal(err)
	}
	msg := res["messages"].([]interface{})[0].(map[string]interface{})
	if _, present := msg["images"]; present {
		t.Error("images should be absent unless include_images is set")
	}

	res, err = s.Poll(context.Background(), map[string]interface{}{
		"thread_ts": "111.000", "include_images": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	msg = res["messages"].([]interface{})[0].(map[string]interface{})
	images, ok := msg["images"].([]interface{})
	if !ok || len(images) != 1 {
		t.Fatalf("expected one relayed image, got %v", msg["images"])
	}
	if uri, _ := images[0].(string); len(uri) < 30 || uri[:23] != "data:image/jpeg;base64," {
		t.Errorf("expected a JPEG data URI, got %.40q", uri)
	}
}

func TestPollReportsUnreachableAttachment(t *testing.T) {
	replies := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "messages": []interface{}{
			map[string]interface{}{"ts": "113.000", "text": "see this", "user": "U9",
				"files": []interface{}{map[string]interface{}{
					"mimetype": "image/png", "url_private": "http://127.0.0.1:1/gone.png",
				}}},
		}})
	}))
	defer replies.Close()

	s := newSlackResource(&Config{BotToken: "xoxb-1", DefaultChannelID: "C0A"}, testLogger())
	s.repliesURL = replies.URL

	res, err := s.Poll(context.Background(), map[string]interface{}{
		"thread_ts": "111.000", "include_images": true,
	})
	if err != nil {
		t.Fatalf("an unreachable attachment must not fail the poll: %v", err)
	}
	msgs := res["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("the message must survive its attachment, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	if msg["text"] != "see this" {
		t.Errorf("message text lost: %v", msg["text"])
	}
	if errs, ok := msg["image_errors"].([]interface{}); !ok || len(errs) != 1 {
		t.Fatalf("expected the failure reported, got %v", msg["image_errors"])
	}
}
