//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/bundle"
)

func TestInstallRetiresOnlyFixedLegacyEdgeUnits(t *testing.T) {
	original := systemctlCommand
	t.Cleanup(func() { systemctlCommand = original })
	var calls [][]string
	systemctlCommand = func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 3 && args[0] == "show" && args[1] == "mcp-devbox-opencode-edge.service" {
			return []byte("not-found\n"), nil
		}
		if len(args) >= 3 && args[0] == "show" {
			return []byte("loaded\n"), nil
		}
		return nil, nil
	}
	if err := retireLegacyEdgeServices(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"show", "mcp-devbox-edge.service", "--property=LoadState", "--value"},
		{"disable", "--now", "mcp-devbox-edge.service"},
		{"show", "mcp-devbox-opencode-edge.service", "--property=LoadState", "--value"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
	systemctlCommand = func(args ...string) ([]byte, error) { return nil, errors.New("failed") }
	if err := retireLegacyEdgeServices(); err == nil {
		t.Fatal("legacy service inspection failure accepted")
	}
}

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
