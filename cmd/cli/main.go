// Command cli is a small harness for exercising a notifier locally without a
// full Viam machine. Configure a model and call DoCommand directly.
package main

import (
	"context"

	"go.viam.com/rdk/logging"

	"notifications/models/slack"
	"notifications/notify"
)

func main() {
	if err := realMain(); err != nil {
		panic(err)
	}
}

func realMain() error {
	ctx := context.Background()
	logger := logging.NewLogger("cli")

	// Pick a Sender implementation and drive it directly. Swap slack.New for a
	// future backend (email, sms, ...) to test it the same way.
	var sender notify.Sender = slack.New(&slack.Config{
		// BotToken: "xoxb-...",
		// DefaultChannel: "#general",
	}, logger)

	_, err := sender.Send(ctx, map[string]interface{}{
		"text": "hello from the notifications module cli",
	})
	return err
}
