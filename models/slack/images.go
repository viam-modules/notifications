package slack

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registered so Decode handles GIF attachments
	"image/jpeg"
	_ "image/png" // registered so Decode handles PNG attachments
	"io"
	"net/http"

	xdraw "golang.org/x/image/draw"
)

const (
	// Attachments are re-encoded to bound what a caller receives: a pasted
	// screenshot is routinely several megabytes, and a caller on a machine is
	// often on a constrained link with limited memory. 1280px/JPEG80 is
	// indistinguishable on screen at a fraction of the size.
	maxImageDimension = 1280
	jpegQuality       = 80
	maxDownloadBytes  = 25 << 20
)

// fetchImage downloads a Slack-hosted file and returns it as a data URI.
//
// url_private requires the bot token in an Authorization header, which is why a
// browser cannot fetch these itself: the request is cross-origin and the token
// must not reach a client.
func (s *slack) fetchImage(ctx context.Context, urlPrivate string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlPrivate, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.BotToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("slack: fetching attachment: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("slack: fetching attachment: http %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes))
	if err != nil {
		return "", fmt.Errorf("slack: reading attachment: %w", err)
	}
	shrunk, err := shrinkToJPEG(raw)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(shrunk), nil
}

// shrinkToJPEG bounds an image's longest edge to maxImageDimension and
// re-encodes it as JPEG. It re-encodes even when no resize is needed, so a
// caller gets one predictable format and a known size ceiling.
func shrinkToJPEG(raw []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("slack: decoding attachment: %w", err)
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("slack: decoding attachment: empty bounds")
	}

	if w > maxImageDimension || h > maxImageDimension {
		if w >= h {
			h = max(h*maxImageDimension/w, 1)
			w = maxImageDimension
		} else {
			w = max(w*maxImageDimension/h, 1)
			h = maxImageDimension
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
		src = dst
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("slack: encoding attachment: %w", err)
	}
	return buf.Bytes(), nil
}
