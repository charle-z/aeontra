package workqueue

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestorePreservesJobsAndRejectsOverwrite(t *testing.T) {
	store := openTestStore(t, Config{})
	job, _, err := store.Enqueue(testSpec("backup-job-0001", "alpha"))
	if err != nil {
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
