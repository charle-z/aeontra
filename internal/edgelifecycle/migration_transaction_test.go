//go:build !windows

package edgelifecycle

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPreparedLegacyStateMigrationCanFinalizeAfterServiceHealth(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	writeFixtureFile(t, filepath.Join(legacy, "workspaces.db"), "workspace-state")
	before := snapshotTree(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{RetainVerifiedJournal: true})
	if err != nil || result.Status != MigrationStatusPrepared {
		t.Fatalf("prepare result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(plan.JournalPath); err != nil {
		t.Fatalf("verified journal missing: %v", err)
	}
	if after := snapshotTree(t, plan.Destination); !reflect.DeepEqual(before, after) {
		t.Fatalf("prepared migration changed state\nbefore=%v\nafter=%v", before, after)
	}

	result, err = FinalizeLegacyStateMigration(config)
	if err != nil || result.Status != MigrationStatusMigrated {
		t.Fatalf("finalize result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
		t.Fatalf("journal remained after finalize: %v", err)
	}
	result, err = FinalizeLegacyStateMigration(config)
	if err != nil || result.Status != MigrationStatusNotNeeded {
		t.Fatalf("repeat finalize result=%+v err=%v", result, err)
	}
}

func TestPreparedLegacyStateMigrationCanRollbackAfterPackageFailure(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	writeFixtureFile(t, filepath.Join(legacy, "checkpoint.md"), "keep")
	before := snapshotTree(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{RetainVerifiedJournal: true})
	if err != nil || result.Status != MigrationStatusPrepared {
		t.Fatalf("prepare result=%+v err=%v", result, err)
	}

	result, err = RollbackPreparedLegacyStateMigration(config)
	if err != nil || result.Status != MigrationStatusRecoveredRollback {
		t.Fatalf("rollback result=%+v err=%v", result, err)
	}
	if after := snapshotTree(t, legacy); !reflect.DeepEqual(before, after) {
		t.Fatalf("package rollback changed legacy state\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Lstat(plan.Destination); !os.IsNotExist(err) {
		t.Fatalf("destination remained after rollback: %v", err)
	}
	if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
		t.Fatalf("journal remained after rollback: %v", err)
	}
}

func TestFinalizeAndPackageRollbackRejectUnverifiedJournal(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(StateMigrationConfig) (StateMigrationResult, error)
	}{
		{name: "finalize", run: FinalizeLegacyStateMigration},
		{name: "rollback", run: RollbackPreparedLegacyStateMigration},
	} {
		t.Run(operation.name, func(t *testing.T) {
			home := t.TempDir()
			legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
			writeValidEdgeState(t, legacy)
			config := testMigrationConfig(home)
			plan, err := PlanLegacyStateMigration(config)
			if err != nil {
				t.Fatal(err)
			}
			prepareMigrationParent(t, plan)
			writeMigrationJournalForTest(t, plan, MigrationStagePrepared)
			if _, err := operation.run(config); !hasMigrationCode(err, MigrationErrorJournalInvalid) {
				t.Fatalf("err=%v", err)
			}
			if _, err := os.Lstat(plan.JournalPath); err != nil {
				t.Fatalf("unverified journal removed: %v", err)
			}
		})
	}
}

func TestPreparedMigrationStillRollsBackInjectedVerificationFailure(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	before := snapshotTree(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ApplyLegacyStateMigration(config, plan, MigrationHooks{
		RetainVerifiedJournal: true,
		AfterVerify:           failingMigrationHook,
	})
	if err == nil {
		t.Fatal("verification failure ignored")
	}
	if after := snapshotTree(t, legacy); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback changed source\nbefore=%v\nafter=%v", before, after)
	}
}
