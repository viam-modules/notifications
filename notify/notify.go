// Package notify defines the shared contract for the notifications module.
//
// Every notification backend (slack, email, sms, ...) is exposed as a Viam
// generic service and implements Sender. Callers trigger a notification with
// DoCommand; HandleDoCommand routes that generic payload to the backend so each
// model only has to implement Send.
package notify

import (
	"context"
	"fmt"
)

// Sender delivers a single notification described by a free-form payload and
// returns a free-form result. Each backend interprets the payload keys it
// understands. For example, slack reads "channel_id"/"text"; an email backend
// would read "to"/"subject"/"body".
type Sender interface {
	Send(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
}

// Reactor adds an emoji reaction to a message a backend previously sent,
// identified by the free-form payload. The convention is that React accepts the
// same message-identity keys Send returns, so a caller can hand Send's result
// back with an added reaction name without interpreting it. Backends that
// support reactions implement it; those that don't simply omit it and the
// "react" command reports the backend as unsupported. For example, slack reads
// "ts" and "channel" (as returned by Send) plus "name" (the emoji name).
type Reactor interface {
	React(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
}

// HandleDoCommand routes a generic service DoCommand to a Sender.
//
// The optional "command" key selects the operation. "send" (also the default
// when omitted) delivers a notification; "react" adds an emoji reaction and
// requires the backend to implement Reactor.
func HandleDoCommand(ctx context.Context, s Sender, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, _ := cmd["command"].(string)
	switch command {
	case "", "send":
		return s.Send(ctx, cmd)
	case "react":
		r, ok := s.(Reactor)
		if !ok {
			return nil, fmt.Errorf("notifications: %q is not supported by this backend", command)
		}
		return r.React(ctx, cmd)
	default:
		return nil, fmt.Errorf("notifications: unknown command %q", command)
	}
}
