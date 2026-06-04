package main

import (
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"

	"notifications/models/slack"
)

func main() {
	// ModularMain can take multiple APIModel arguments. Add a line per model as
	// new notification backends (email, sms, ...) are implemented.
	module.ModularMain(
		resource.APIModel{API: generic.API, Model: slack.Model},
	)
}
