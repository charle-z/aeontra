//go:build !windows

package edgelifecycle

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestPlanLegacyStateMigrationIsReadOnlyAndExact(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	before := snapshotTree(t, legacy)
	config := testMigrationConfig(home)

	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Needed || plan.Version != StateMigrationVersion || plan.Kind != MigrationLegacyToPreferred {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Source != legacy || plan.Destination != filepath.Join(home, ".local", "state", "mcp-edge") || plan.JournalPath != filepath.Join(home, ".local", "state", stateMigrationJournalName) {
		t.Fatalf("unexpected plan paths: %+v", plan)
	}
	if after := snapshotTree(t, legacy); !reflect.DeepEqual(before, after) {
		t.Fatalf("planning mutated source\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Lstat(plan.Destination); !os.IsNotExist(err) {
		t.Fatalf("planning created destination: %v", err)
	}
	if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
		t.Fatalf("planning created journal: %v", err)
	}
}

func TestApplyLegacyStateMigrationPreservesStateAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	writeFixtureFile(t, filepath.Join(legacy, "workspaces.db"), "ws_593c26b24ba6dc583c9aa1da5e9e0152")
	before := snapshotTree(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}

	result, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != MigrationStatusMigrated {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source still exists: %v", err)
	}
	if after := snapshotTree(t, plan.Destination); !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed state\nbefore=%v\nafter=%v", before, after)
	}
	if _, _, err := edgeclient.LoadIdentity(plan.Destination); err != nil {
		t.Fatalf("migrated identity invalid: %v", err)
	}
	if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
		t.Fatalf("journal remained after success: %v", err)
	}

	repeat, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.Needed {
		t.Fatalf("repeat plan still needs migration: %+v", repeat)
	}
	result, err = ApplyLegacyStateMigration(config, repeat, MigrationHooks{})
	if err != nil || result.Status != MigrationStatusNotNeeded {
		t.Fatalf("repeat result=%+v err=%v", result, err)
	}
}

func TestApplyLegacyStateMigrationRollsBackEveryInjectedFailure(t *testing.T) {
	stages := []struct {
		name  string
		hooks MigrationHooks
	}{
		{name: "after journal", hooks: MigrationHooks{AfterJournal: failingMigrationHook}},
		{name: "after rename", hooks: MigrationHooks{AfterRename: failingMigrationHook}},
		{name: "after verify", hooks: MigrationHooks{AfterVerify: failingMigrationHook}},
	}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
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

			if _, err := ApplyLegacyStateMigration(config, plan, stage.hooks); err == nil {
				t.Fatal("injected migration failure was ignored")
			}
			if after := snapshotTree(t, legacy); !reflect.DeepEqual(before, after) {
				t.Fatalf("rollback changed source\nbefore=%v\nafter=%v", before, after)
			}
			if _, err := os.Lstat(plan.Destination); !os.IsNotExist(err) {
				t.Fatalf("destination remained after rollback: %v", err)
			}
			if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
				t.Fatalf("journal remained after rollback: %v", err)
			}
		})
	}
}

func TestApplyLegacyStateMigrationRejectsStaleOrTamperedPlan(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
	writeValidEdgeState(t, legacy)
	config := testMigrationConfig(home)
	plan, err := PlanLegacyStateMigration(config)
	if err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*StateMigrationPlan){
		"version":     func(p *StateMigrationPlan) { p.Version++ },
		"source":      func(p *StateMigrationPlan) { p.Source = filepath.Join(home, "p12") },
		"destination": func(p *StateMigrationPlan) { p.Destination = filepath.Join(home, "other") },
		"journal":     func(p *StateMigrationPlan) { p.JournalPath = filepath.Join(home, "journal") },
		"kind":        func(p *StateMigrationPlan) { p.Kind = MigrationNone },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := plan
			mutate(&candidate)
			if _, err := ApplyLegacyStateMigration(config, candidate, MigrationHooks{}); !hasMigrationCode(err, MigrationErrorPlanChanged) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	if _, _, err := edgeclient.LoadIdentity(legacy); err != nil {
		t.Fatalf("tampered plan changed source: %v", err)
	}
}

func TestPlanLegacyStateMigrationRejectsInvalidIdentityPermissionsAndOwner(t *testing.T) {
	t.Run("invalid identity", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeFixtureFile(t, filepath.Join(legacy, "identity.json"), "not-json")
		writeFixtureFile(t, filepath.Join(legacy, "device.key"), "not-key")
		if _, err := PlanLegacyStateMigration(testMigrationConfig(home)); !hasMigrationCode(err, MigrationErrorStateInvalid) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("unsafe mode", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeValidEdgeState(t, legacy)
		if err := os.Chmod(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := PlanLegacyStateMigration(testMigrationConfig(home)); !hasMigrationCode(err, MigrationErrorStateInvalid) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("wrong expected owner", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeValidEdgeState(t, legacy)
		config := testMigrationConfig(home)
		config.ExpectedUID++
		if _, err := PlanLegacyStateMigration(config); !hasMigrationCode(err, MigrationErrorOwnerMismatch) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestRecoverLegacyStateMigrationHandlesPreparedAndRenamedStages(t *testing.T) {
	t.Run("prepared without rename", func(t *testing.T) {
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

		result, err := RecoverLegacyStateMigration(config)
		if err != nil || result.Status != MigrationStatusRecoveredRollback {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if _, _, err := edgeclient.LoadIdentity(legacy); err != nil {
			t.Fatalf("source unavailable after recovery: %v", err)
		}
		if _, err := os.Lstat(plan.JournalPath); !os.IsNotExist(err) {
			t.Fatalf("journal remained: %v", err)
		}
	})

	t.Run("renamed before journal update", func(t *testing.T) {
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
		if err := renameNoReplace(plan.Source, plan.Destination); err != nil {
			t.Fatal(err)
		}

		result, err := RecoverLegacyStateMigration(config)
		if err != nil || result.Status != MigrationStatusRecoveredComplete {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if _, _, err := edgeclient.LoadIdentity(plan.Destination); err != nil {
			t.Fatalf("destination unavailable after recovery: %v", err)
		}
		if _, err := os.Lstat(plan.Source); !os.IsNotExist(err) {
			t.Fatalf("source returned unexpectedly: %v", err)
		}
	})
}

func TestRecoverLegacyStateMigrationRejectsAmbiguousAndTamperedJournal(t *testing.T) {
	t.Run("both source and destination exist", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeValidEdgeState(t, legacy)
		config := testMigrationConfig(home)
		plan, err := PlanLegacyStateMigration(config)
		if err != nil {
			t.Fatal(err)
		}
		prepareMigrationParent(t, plan)
		writeMigrationJournalForTest(t, plan, MigrationStageRenamed)
		writeValidEdgeState(t, plan.Destination)

		if _, err := RecoverLegacyStateMigration(config); !hasMigrationCode(err, MigrationErrorRecoveryAmbiguous) {
			t.Fatalf("err=%v", err)
		}
		if _, err := os.Lstat(plan.JournalPath); err != nil {
			t.Fatalf("ambiguous journal was removed: %v", err)
		}
	})

	t.Run("journal path changed", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeValidEdgeState(t, legacy)
		config := testMigrationConfig(home)
		plan, err := PlanLegacyStateMigration(config)
		if err != nil {
			t.Fatal(err)
		}
		prepareMigrationParent(t, plan)
		journal := newMigrationJournal(plan, MigrationStagePrepared, time.Unix(1, 0).UTC())
		journal.Destination = filepath.Join(home, "p12")
		writeJournalFixture(t, plan.JournalPath, journal)
		if _, err := RecoverLegacyStateMigration(config); !hasMigrationCode(err, MigrationErrorJournalInvalid) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestApplyLegacyStateMigrationBlocksExistingJournalAndDestination(t *testing.T) {
	t.Run("existing journal", func(t *testing.T) {
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
		if _, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{}); !hasMigrationCode(err, MigrationErrorJournalExists) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("destination appears after planning", func(t *testing.T) {
		home := t.TempDir()
		legacy := filepath.Join(home, ".config", "mcp-devbox-edge")
		writeValidEdgeState(t, legacy)
		config := testMigrationConfig(home)
		plan, err := PlanLegacyStateMigration(config)
		if err != nil {
			t.Fatal(err)
		}
		writeValidEdgeState(t, plan.Destination)
		if _, err := ApplyLegacyStateMigration(config, plan, MigrationHooks{}); !hasMigrationCode(err, MigrationErrorPlanChanged) {
			t.Fatalf("err=%v", err)
		}
	})
}

func testMigrationConfig(home string) StateMigrationConfig {
	return StateMigrationConfig{
		Inventory:   InventoryConfig{HomeDir: home, InstallRoot: filepath.Join(home, "opt", "mcp-devbox")},
		ExpectedUID: currentEUID(),
		ExpectedGID: currentEGID(),
		Now:         func() time.Time { return time.Unix(1_721_665_200, 0).UTC() },
	}
}

func writeValidEdgeState(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	controlPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := edgeclient.Identity{
		SchemaVersion:    2,
		ServerURL:        "https://mcp.example.com",
		DeviceID:         "ed_0123456789abcdef0123456789abcdef",
		Name:             "parrot-edge",
		ControlPublicKey: edge.EncodePublicKey(controlPublic),
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "identity.json"), append(identityBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	key := base64.RawURLEncoding.EncodeToString(privateKey) + "\n"
	if err := os.WriteFile(filepath.Join(root, "device.key"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
}

func failingMigrationHook() error { return errors.New("injected failure") }

func prepareMigrationParent(t *testing.T, plan StateMigrationPlan) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(plan.Destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(plan.Destination), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeMigrationJournalForTest(t *testing.T, plan StateMigrationPlan, stage MigrationStage) {
	t.Helper()
	writeJournalFixture(t, plan.JournalPath, newMigrationJournal(plan, stage, time.Unix(1, 0).UTC()))
}

func writeJournalFixture(t *testing.T, path string, journal migrationJournal) {
	t.Helper()
	content, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasMigrationCode(err error, code MigrationErrorCode) bool {
	var migrationErr *MigrationError
	return errors.As(err, &migrationErr) && migrationErr.Code == code
}
