//go:build !windows

package edgelifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverLegacyStateMigrationIsNoopWithoutJournal(t *testing.T) {
	home := t.TempDir()
	result, err := RecoverLegacyStateMigration(testMigrationConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != MigrationStatusNotNeeded {
		t.Fatalf("result=%+v", result)
	}
}

func TestRecoverLegacyStateMigrationRejectsSymlinkedJournal(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	prepareMigrationParent(t, plan)
	target := filepath.Join(home, "journal-target")
	writeFixtureFile(t, target, "{}")
	if err := os.Symlink(target, plan.JournalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverLegacyStateMigration(config); !hasMigrationCode(err, MigrationErrorJournalInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyLegacyStateMigrationRejectsAncestorSwapAfterPlanning(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(home, ".local")
	outside := t.TempDir()
	if err := os.Symlink(outside, local); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{}); !hasMigrationCode(err, MigrationErrorInventoryBlocked) {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := loadMigratedIdentityForTest(legacy); err != nil {
		t.Fatalf("source changed after ancestor swap: %v", err)
	}
}

func TestApplyLegacyStateMigrationRequiresEdgeOwnerProcess(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	config.ExpectedUID++
	if _, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{}); !hasMigrationCode(err, MigrationErrorWrongExecutor) {
		t.Fatalf("err=%v", err)
	}
}

func loadMigratedIdentityForTest(root string) (string, bool, error) {
	info, err := os.Lstat(filepath.Join(root, "identity.json"))
	if err != nil {
		return "", false, err
	}
	return root, info.Mode().IsRegular(), nil
}
