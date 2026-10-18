//go:build !windows

package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgelifecycle"
)

func TestFormatLifecycleInventoryOmitsPathsTargetsAndIdentity(t *testing.T) {
	report := edgelifecycle.LayoutReport{
		PreferredState:  edgelifecycle.PathStatus{Path: "/home/charles/.local/state/mcp-edge", Kind: edgelifecycle.PathEdgeState, IdentityPresent: true},
		LegacyState:     edgelifecycle.PathStatus{Path: "/home/charles/.config/mcp-devbox-edge", Kind: edgelifecycle.PathMissing},
		DevelopmentRoot: edgelifecycle.PathStatus{Path: "/home/charles/workspaces", Kind: edgelifecycle.PathDirectory},
		LabRoot:         edgelifecycle.PathStatus{Path: "/home/charles/htb-machines", Kind: edgelifecycle.PathDirectory},
		CurrentRelease:  edgelifecycle.PathStatus{Path: "/opt/mcp-devbox/current", Kind: edgelifecycle.PathSymlink, Target: "releases/p15.0.5"},
		Historical:      []edgelifecycle.PathStatus{{Path: "/home/charles/p12", Kind: edgelifecycle.PathUnknownDirectory}},
		StateMigration:  edgelifecycle.MigrationNone,
		Blockers:        []edgelifecycle.Blocker{{Code: edgelifecycle.BlockerPreferredStateOccupied, Subject: "preferred_state"}},
	}
	output := formatLifecycleInventory(report)
	for _, secret := range []string{"/home/charles", "/opt/mcp-devbox", "p15.0.5", "releases/p15.0.5"} {
		if strings.Contains(output, secret) {
			t.Fatalf("safe lifecycle output exposed %q: %s", secret, output)
		}
	}
	for _, expected := range []string{"preferred=edge_state", "historical=unknown_directory", "blockers=preferred_state_occupied"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q: %s", expected, output)
		}
	}
}

func TestLifecycleCommandUsesClosedNoArgumentOperations(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"inspect", "extra"},
		{"migrate-state", "--home", "/tmp"},
		{"recover-state", "extra"},
		{"unknown"},
	} {
		if err := lifecycleCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("unsafe lifecycle args accepted: %v", args)
		}
	}
}

func TestLifecycleMigrationAndRecoveryReturnOnlyStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldPlan := planEdgeStateMigration
	oldApply := applyEdgeStateMigration
	oldRecover := recoverEdgeStateMigration
	t.Cleanup(func() {
		planEdgeStateMigration = oldPlan
		applyEdgeStateMigration = oldApply
		recoverEdgeStateMigration = oldRecover
	})

	planEdgeStateMigration = func(config edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationPlan, error) {
		if config.Inventory.HomeDir != home || config.Inventory.InstallRoot != installedBundleRoot || len(config.Inventory.HistoricalPaths) != 1 || config.Inventory.HistoricalPaths[0] != filepath.Join(home, "p12") {
			return edgelifecycle.StateMigrationPlan{}, errors.New("unexpected lifecycle config")
		}
		return edgelifecycle.StateMigrationPlan{Version: edgelifecycle.StateMigrationVersion, Needed: true, Kind: edgelifecycle.MigrationLegacyToPreferred}, nil
	}
	applyEdgeStateMigration = func(edgelifecycle.StateMigrationConfig, edgelifecycle.StateMigrationPlan, edgelifecycle.MigrationHooks) (edgelifecycle.StateMigrationResult, error) {
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusMigrated}, nil
	}
	recoverEdgeStateMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationResult, error) {
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusRecoveredComplete}, nil
	}

	var stdout bytes.Buffer
	if err := lifecycleCommand([]string{"migrate-state"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "edge_lifecycle migration=migrated\n" {
		t.Fatalf("migration output=%q", stdout.String())
	}
	stdout.Reset()
	if err := lifecycleCommand([]string{"recover-state"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "edge_lifecycle recovery=recovered_complete\n" {
		t.Fatalf("recovery output=%q", stdout.String())
	}
}

func TestLifecycleInspectReturnsSafeSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldInspect := inspectEdgeLifecycle
	t.Cleanup(func() { inspectEdgeLifecycle = oldInspect })
	inspectEdgeLifecycle = func(config edgelifecycle.InventoryConfig) (edgelifecycle.LayoutReport, error) {
		return edgelifecycle.LayoutReport{
			PreferredState: edgelifecycle.PathStatus{Path: filepath.Join(home, "secret"), Kind: edgelifecycle.PathMissing},
			LegacyState:    edgelifecycle.PathStatus{Kind: edgelifecycle.PathEdgeState},
			StateMigration: edgelifecycle.MigrationLegacyToPreferred,
		}, nil
	}
	var stdout bytes.Buffer
	if err := lifecycleCommand([]string{"inspect"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), home) || !strings.Contains(stdout.String(), "migration=legacy_to_preferred") {
		t.Fatalf("unsafe or incomplete output=%q", stdout.String())
	}
}
