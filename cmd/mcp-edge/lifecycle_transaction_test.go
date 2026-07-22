//go:build !windows

package main

import (
	"bytes"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgelifecycle"
)

func TestLifecyclePreparedPackageTransactionUsesClosedCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldPlan := planEdgeStateMigration
	oldApply := applyEdgeStateMigration
	oldFinalize := finalizeEdgeStateMigration
	oldRollback := rollbackEdgeStateMigration
	t.Cleanup(func() {
		planEdgeStateMigration = oldPlan
		applyEdgeStateMigration = oldApply
		finalizeEdgeStateMigration = oldFinalize
		rollbackEdgeStateMigration = oldRollback
	})

	planEdgeStateMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationPlan, error) {
		return edgelifecycle.StateMigrationPlan{Version: edgelifecycle.StateMigrationVersion, Needed: true, Kind: edgelifecycle.MigrationLegacyToPreferred}, nil
	}
	applyEdgeStateMigration = func(_ edgelifecycle.StateMigrationConfig, _ edgelifecycle.StateMigrationPlan, hooks edgelifecycle.MigrationHooks) (edgelifecycle.StateMigrationResult, error) {
		if !hooks.RetainVerifiedJournal {
			t.Fatal("package preparation did not retain verified journal")
		}
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusPrepared}, nil
	}
	finalizeEdgeStateMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationResult, error) {
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusMigrated}, nil
	}
	rollbackEdgeStateMigration = func(edgelifecycle.StateMigrationConfig) (edgelifecycle.StateMigrationResult, error) {
		return edgelifecycle.StateMigrationResult{Status: edgelifecycle.MigrationStatusRecoveredRollback}, nil
	}

	for _, item := range []struct {
		command []string
		want    string
	}{
		{command: []string{"prepare-state-migration"}, want: "edge_lifecycle migration=prepared\n"},
		{command: []string{"finalize-state-migration"}, want: "edge_lifecycle finalization=migrated\n"},
		{command: []string{"rollback-state-migration"}, want: "edge_lifecycle rollback=recovered_rollback\n"},
	} {
		var stdout bytes.Buffer
		if err := lifecycleCommand(item.command, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatalf("command=%v err=%v", item.command, err)
		}
		if stdout.String() != item.want {
			t.Fatalf("command=%v output=%q want=%q", item.command, stdout.String(), item.want)
		}
	}
}

func TestLifecyclePackageTransactionCommandsRejectArguments(t *testing.T) {
	for _, command := range []string{"prepare-state-migration", "finalize-state-migration", "rollback-state-migration"} {
		if err := lifecycleCommand([]string{command, "extra"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("%s accepted an argument", command)
		}
	}
}
