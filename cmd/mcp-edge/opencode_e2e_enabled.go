//go:build opencode_e2e && !windows

package main

import (
	"errors"
	"os"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func configureOpenCodeRelayE2E(launcher *edgeclient.OpenCodeLauncher) error {
	value := os.Getenv("MCP_DEVBOX_RELAY_CONTAINER_E2E")
	if value == "" {
		return nil
	}
	if value != "1" {
		return errors.New("relay container E2E mode is invalid")
	}
	return edgeclient.EnableRelayContainerE2E(launcher)
}
