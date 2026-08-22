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
		{"disable", "mcp-devbox-edge.service"},
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

func TestServiceGenerationKeepsRollbackAndSelectsNeutralCodexUnit(t *testing.T) {
	base, relative := serviceGeneration(4)
	if base != legacyServiceBase || relative != "systemd/mcp-devbox-opencode-edge@.service" {
		t.Fatalf("v4 service=%q path=%q", base, relative)
	}
	base, relative = serviceGeneration(5)
	if base != currentServiceBase || relative != "systemd/mcp-devbox-edge@.service" {
		t.Fatalf("v5 service=%q path=%q", base, relative)
	}
}

func TestOfficialComponentLinksTrackGenerationAndRollback(t *testing.T) {
	root := t.TempDir()
	links := []officialComponentLink{
		{bundle.ComponentEdge, filepath.Join(root, "bin", "mcp-edge"), filepath.Join(root, "current", "bin", "mcp-edge")},
		{bundle.ComponentNode, filepath.Join(root, "libexec", "node"), filepath.Join(root, "current", "libexec", "node")},
		{bundle.ComponentProvider, filepath.Join(root, "opencode-provider"), filepath.Join(root, "current", "opencode-provider")},
		{bundle.ComponentOpenCode, filepath.Join(root, "opencode"), filepath.Join(root, "current", "opencode")},
	}
	v4, ok := bundle.LayoutForVersion(4)
	if !ok {
		t.Fatal("v4 layout unavailable")
	}
	v5, ok := bundle.LayoutForVersion(5)
	if !ok {
		t.Fatal("v5 layout unavailable")
	}
	if err := reconcileOfficialComponentLinks(v4, links); err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if got, err := os.Readlink(link.destination); err != nil || got != link.target {
			t.Fatalf("v4 link %s=%q err=%v", link.component, got, err)
		}
	}
	if err := reconcileOfficialComponentLinks(v5, links); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(links[0].destination); err != nil || got != links[0].target {
		t.Fatalf("shared Edge link=%q err=%v", got, err)
	}
	for _, link := range links[1:] {
		if _, err := os.Lstat(link.destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("v5 retained %s link: %v", link.component, err)
		}
	}
	if err := reconcileOfficialComponentLinks(v4, links); err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if got, err := os.Readlink(link.destination); err != nil || got != link.target {
			t.Fatalf("rollback link %s=%q err=%v", link.component, got, err)
		}
	}
}

func TestAbsentComponentPreservesUnmanagedCompatibilityPath(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "opencode-provider")
	foreignTarget := filepath.Join(root, "operator-provider")
	if err := os.Symlink(foreignTarget, destination); err != nil {
		t.Fatal(err)
	}
	v5, ok := bundle.LayoutForVersion(5)
	if !ok {
		t.Fatal("v5 layout unavailable")
	}
	links := []officialComponentLink{{bundle.ComponentProvider, destination, filepath.Join(root, "current", "opencode-provider")}}
	if err := reconcileOfficialComponentLinks(v5, links); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(destination); err != nil || got != foreignTarget {
		t.Fatalf("unmanaged link=%q err=%v", got, err)
	}
}

func TestTrustedManifestOutranksStaleInstalledUnitDuringRollback(t *testing.T) {
	if got := serviceBaseFromEvidence(4, true, true); got != legacyServiceBase {
		t.Fatalf("trusted v4 selected %q, want rollback service", got)
	}
	if got := serviceBaseFromEvidence(5, true, false); got != currentServiceBase {
		t.Fatalf("trusted v5 selected %q, want neutral service", got)
	}
	if got := serviceBaseFromEvidence(0, false, true); got != currentServiceBase {
		t.Fatalf("unreadable manifest fallback selected %q, want installed neutral service", got)
	}
}
