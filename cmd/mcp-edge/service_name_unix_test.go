//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEdgeServiceNameTracksSignedBundleGeneration(t *testing.T) {
	root := t.TempDir()
	if got := edgeServiceNameAt(root, "charles"); got != "mcp-devbox-opencode-edge@charles.service" {
		t.Fatalf("legacy service=%q", got)
	}
	path := filepath.Join(root, "systemd", "mcp-devbox-edge@.service")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := edgeServiceNameAt(root, "charles"); got != "mcp-devbox-edge@charles.service" {
		t.Fatalf("current service=%q", got)
	}
}
