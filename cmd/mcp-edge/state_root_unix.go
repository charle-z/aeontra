//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func platformDefaultStateRoot(home string) string {
	stateBase := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateBase == "" || !filepath.IsAbs(stateBase) {
		stateBase = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateBase, "mcp-edge")
}
