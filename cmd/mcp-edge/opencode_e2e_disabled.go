//go:build !opencode_e2e && !windows

package main

import "github.com/charle-z/mcp-devbox/internal/edgeclient"

func configureOpenCodeRelayE2E(*edgeclient.OpenCodeLauncher) error { return nil }
