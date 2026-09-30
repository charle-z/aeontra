package workqueue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestBackupRestorePreservesJobsAndRejectsOverwrite(t *testing.T) {
	store := openTestStore(t, Config{})
	job, _, err := store.Enqueue(testSpec("backup-job-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := development.NewResolutionPolicy(development.TierWorkcell, development.ClassWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := development.Requirements("toolchain.go")
	if err != nil {
		t.Fatal(err)
	}
	objective, err := development.NewObjective("backup-objective-1", policy, []development.StepSpec{{
		StepID: "validate", Requirements: requirements,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.SaveDevelopmentObjective(objective); err != nil || !created {
		t.Fatalf("initial objective created=%t err=%v", created, err)
	}
	capabilities, err := development.NewCapabilitySet("toolchain.go")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := development.NewEnvironmentAttestation("l3", development.ClassWorkcell, 1, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	objective, _, err = objective.PlanAttempt("validate", "backup-attempt-1", "sha256:"+strings.Repeat("a", 64), []development.EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.SaveDevelopmentObjective(objective); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(t.TempDir(), "backup")
	backupPath, err := store.Backup(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(backupPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup info=%v err=%v", info, err)
	}
	if _, err := store.Backup(backupRoot); err == nil || err.Error() != "workqueue: backup destination already exists" {
		t.Fatalf("repeat backup err=%v", err)
	}
	restoreRoot := filepath.Join(t.TempDir(), "restored")
	restored, err := RestoreBackup(backupPath, Config{Root: restoreRoot, ControllerID: "control-plane"})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, found, err := restored.Get(job.ID)
	if err != nil || !found || got.ID != job.ID || got.PayloadHash != job.PayloadHash {
		t.Fatalf("restored=%+v found=%v err=%v", got, found, err)
	}
	restoredObjective, found, err := restored.DevelopmentObjective(objective.ObjectiveID)
	if err != nil || !found || restoredObjective.Revision != objective.Revision ||
		len(restoredObjective.Steps) != 1 || len(restoredObjective.Steps[0].Attempts) != 1 ||
		restoredObjective.Steps[0].Attempts[0].EnvironmentDigest != environment.Digest {
		t.Fatalf("restored objective=%+v found=%v err=%v", restoredObjective, found, err)
	}
	if _, err := RestoreBackup(backupPath, Config{Root: restoreRoot, ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: restore destination is occupied" {
		t.Fatalf("occupied restore err=%v", err)
	}
}

func TestRestoreRejectsSymlinkAndInsecureBackup(t *testing.T) {
	root := t.TempDir()
	unsafe := filepath.Join(root, "unsafe.db")
	if err := os.WriteFile(unsafe, []byte("not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(unsafe, Config{Root: filepath.Join(root, "target"), ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: backup is invalid" {
		t.Fatalf("unsafe backup err=%v", err)
	}
	link := filepath.Join(root, "link.db")
	if err := os.Symlink(unsafe, link); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(link, Config{Root: filepath.Join(root, "target-link"), ControllerID: "control-plane"}); err == nil || err.Error() != "workqueue: backup is invalid" {
		t.Fatalf("symlink backup err=%v", err)
	}
}
