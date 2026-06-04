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
// understands. For example, slack reads "channel"/"text"; an email backend
// would read "to"/"subject"/"body".
type Sender interface {
	Send(ctx context.Context, payload map[string]interface{}) (map[string]interface{}, error)
}

// HandleDoCommand routes a generic service DoCommand to a Sender.
//
// The optional "command" key selects the operation. Today the only command is
// "send" (also the default when omitted), leaving room for future verbs such as
// "validate" without breaking existing callers.
func HandleDoCommand(ctx context.Context, s Sender, cmd map[string]interface{}) (map[string]interface{}, error) {
	command, _ := cmd["command"].(string)
	switch command {
	case "", "send":
		return s.Send(ctx, cmd)
	default:
		return nil, fmt.Errorf("notifications: unknown command %q", command)
	}
}
