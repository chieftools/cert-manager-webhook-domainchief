package main

import (
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

// version is set at build time using -ldflags "-X main.version=...".
var version = "dev"

func main() {
	groupName := os.Getenv("GROUP_NAME")
	if groupName == "" {
		panic("GROUP_NAME must be specified")
	}

	cmd.RunWebhookServer(groupName, &solver{})
}

func userAgent() string {
	return "cert-manager-webhook-domainchief/" + version
}
