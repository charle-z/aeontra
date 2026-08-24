//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func platformDefaultStateRoot(home string) string {
	stateBase := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if stateBase == "" || !filepath.IsAbs(stateBase) {
		stateBase = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(stateBase, "Aeontra", "mcp-edge")
}
