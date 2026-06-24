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

// Reactor optionally adds an emoji reaction to a message a backend previously
// sent, identified by the free-form payload (e.g. slack reads "channel",
// "timestamp" and "name"). Backends that support reactions — such as slack on
// the bot-token path — implement it; those that don't simply omit it and the
// "react" command reports the backend as unsupported.
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
