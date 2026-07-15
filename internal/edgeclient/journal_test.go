package edgeclient

import (
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestJournalMarksStartedBeforeExecutionAndReplaysCompletedResult(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := journal.Begin("journal-task-0001", "et_0123456789abcdef0123456789abcdef")
	if err != nil || entry.State != JournalStarted || !entry.New {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	repeated, err := journal.Begin("journal-task-0001", "et_0123456789abcdef0123456789abcdef")
	if err != nil || repeated.State != JournalStarted || repeated.New {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	result := edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "passed"}
	if err := journal.Finish("journal-task-0001", "et_0123456789abcdef0123456789abcdef", result); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	completed, err := reopened.Begin("journal-task-0001", "et_0123456789abcdef0123456789abcdef")
	if err != nil || completed.State != JournalCompleted || completed.Result != result {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if _, err := reopened.Begin("journal-task-0001", "et_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("idempotency key accepted a different task")
	}
}

func TestJournalRejectsInvalidIdentityAndChangedCompletion(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.Begin("short", "bad"); err == nil {
		t.Fatal("invalid journal entry accepted")
	}
	key := "journal-task-0002"
	taskID := "et_0123456789abcdef0123456789abcdef"
	if _, err := journal.Begin(key, taskID); err != nil {
		t.Fatal(err)
	}
	first := edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "failed safely"}
	if err := journal.Finish(key, taskID, first); err != nil {
		t.Fatal(err)
	}
	if err := journal.Finish(key, taskID, edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "changed"}); err == nil {
		t.Fatal("changed journal completion accepted")
	}
}
