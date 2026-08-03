//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/bundle"
)

func TestUpdaterAcceptsOnlyClosedOperations(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"update", "stable"}, {"rollback"}, {"repair"}} {
		if _, err := parseUpdaterOperation(args); err != nil {
			t.Fatalf("closed operation %v rejected: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"update", "https://attacker.invalid/bundle"},
		{"update", "/tmp/bundle"},
		{"update", "stable", "--hash", "sha256:any"},
		{"sh", "-c", "id"},
		{"install", "script.sh"},
	} {
		if _, err := parseUpdaterOperation(args); err == nil {
			t.Fatalf("open-ended updater operation accepted: %v", args)
		}
	}
}

func TestBundledGitHubCLICompatibilityLinkTracksReleaseAndRollback(t *testing.T) {
	root := t.TempDir()
	releaseRoot := filepath.Join(root, "release")
	destination := filepath.Join(root, "bin", "gh")
	target := filepath.Join(root, "current", "libexec", "gh")
	if err := os.MkdirAll(filepath.Join(releaseRoot, "libexec"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseRoot, "libexec", "gh"), []byte("gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reconcileBundledGitHubCLIAt(releaseRoot, destination, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(destination)
	if err != nil || got != target {
		t.Fatalf("link=%q err=%v", got, err)
	}
	if err := os.Remove(filepath.Join(releaseRoot, "libexec", "gh")); err != nil {
		t.Fatal(err)
	}
	if err := reconcileBundledGitHubCLIAt(releaseRoot, destination, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("managed link survived rollback: %v", err)
	}
}

func TestBridgeRepairAllowsVersionTwoReleaseWithoutBundledGitHubCLI(t *testing.T) {
	root := t.TempDir()
	if err := repairComponentPermissions(root, bundle.ComponentGitHubCLI, "libexec/gh"); err != nil {
		t.Fatal(err)
	}
	if err := repairComponentPermissions(root, bundle.ComponentEdge, "bin/mcp-edge"); err == nil {
		t.Fatal("missing required bridge component unexpectedly accepted")
	}
}
