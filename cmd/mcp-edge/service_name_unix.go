//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func edgeServiceNameAt(bundleRoot, username string) string {
	base := "mcp-devbox-opencode-edge"
	unit := filepath.Join(bundleRoot, "systemd", "mcp-devbox-edge@.service")
	if info, err := os.Lstat(unit); err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		base = "mcp-devbox-edge"
	}
	return base + "@" + username + ".service"
}

func edgeServiceName(username string) string {
	return edgeServiceNameAt(installedBundleRoot, username)
}
