package edgeclient

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestStep4JournalHealthReportsEmptyPendingDeliveredAndReconciliation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if health, err := InspectJournal(root); err != nil || health.State != JournalHealthEmpty {
		t.Fatalf("empty=%+v err=%v", health, err)
	}
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	key := "step4-health-0001"
	taskID := "et_0123456789abcdef0123456789abcdef"
	if _, err := journal.BeginAttempt(key, taskID, 1); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if health, err := InspectJournal(root); err != nil || health.State != JournalHealthReconciliation || health.Started != 1 {
		t.Fatalf("started=%+v err=%v", health, err)
	}

	journal, err = OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := journal.FinishEntry(key, taskID, edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if health, err := InspectJournal(root); err != nil || health.State != JournalHealthPending || health.Pending != 1 {
		t.Fatalf("pending=%+v err=%v", health, err)
	}

	journal, err = OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.MarkDelivered(key, taskID, completed.ResultID); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if health, err := InspectJournal(root); err != nil || health.State != JournalHealthReady || health.Delivered != 1 {
		t.Fatalf("ready=%+v err=%v", health, err)
	}
}

func TestStep4JournalHealthRejectsUnsafeAndCorruptFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "journal.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectJournal(root); err == nil {
		t.Fatal("corrupt journal accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectJournal(root); err == nil {
		t.Fatal("symlink journal accepted")
	}
}
