package edgeclient

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	_ "modernc.org/sqlite"
)

func TestStep4JournalPersistsStableAttemptResultAndDelivery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 7, 23, 23, 30, 0, 0, time.UTC)
	journal.now = func() time.Time { return clock }
	key := "step4-journal-0001"
	taskID := "et_0123456789abcdef0123456789abcdef"

	started, err := journal.BeginAttempt(key, taskID, 7)
	if err != nil || !started.New || started.State != JournalStarted || started.Attempt != 7 || started.ResultID != "" || started.Delivered {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	result := edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "completed locally"}
	completed, err := journal.FinishEntry(key, taskID, result)
	if err != nil || completed.State != JournalCompleted || completed.Result != result || completed.Attempt != 7 || !strings.HasPrefix(completed.ResultID, "jr_") || len(completed.ResultID) != 67 || completed.Delivered {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if err := journal.MarkDelivered(key, taskID, "jr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("wrong result identity marked delivered")
	}
	if err := journal.MarkDelivered(key, taskID, completed.ResultID); err != nil {
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
	replayed, err := reopened.BeginAttempt(key, taskID, 99)
	if err != nil || replayed.New || replayed.State != JournalCompleted || replayed.Result != result || replayed.Attempt != 7 || replayed.ResultID != completed.ResultID || !replayed.Delivered {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}

func TestStep4JournalCleansDeliveredButRetainsPendingAndBoundsCapacity(t *testing.T) {
	journal, err := OpenJournal(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	clock := time.Date(2026, 7, 23, 23, 30, 0, 0, time.UTC)
	journal.now = func() time.Time { return clock }
	journal.maxEntries = 2

	firstKey := "step4-cleanup-0001"
	secondKey := "step4-cleanup-0002"
	firstTask := "et_0123456789abcdef0123456789abcdef"
	secondTask := "et_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	result := edge.TaskResult{Outcome: edge.OutcomeSucceeded, Summary: "done"}
	if _, err := journal.BeginAttempt(firstKey, firstTask, 1); err != nil {
		t.Fatal(err)
	}
	first, err := journal.FinishEntry(firstKey, firstTask, result)
	if err != nil || journal.MarkDelivered(firstKey, firstTask, first.ResultID) != nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := journal.BeginAttempt(secondKey, secondTask, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.FinishEntry(secondKey, secondTask, result); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.BeginAttempt("step4-cleanup-0003", "et_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1); err == nil {
		t.Fatal("journal capacity did not fail closed")
	}
	if existing, err := journal.BeginAttempt(secondKey, secondTask, 8); err != nil || existing.New {
		t.Fatalf("existing entry blocked at capacity: %+v err=%v", existing, err)
	}

	clock = clock.Add(8 * 24 * time.Hour)
	removed, err := journal.CleanupDelivered(7 * 24 * time.Hour)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if pending, err := journal.BeginAttempt(secondKey, secondTask, 9); err != nil || pending.New || pending.Delivered {
		t.Fatalf("pending result was removed: %+v err=%v", pending, err)
	}
	if recreated, err := journal.BeginAttempt(firstKey, firstTask, 2); err != nil || !recreated.New || recreated.Attempt != 2 {
		t.Fatalf("delivered entry was not cleaned: %+v err=%v", recreated, err)
	}
}

func TestStep4JournalMigratesLegacySchemaWithoutLosingCompletion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "journal.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE executions(idempotency_key TEXT PRIMARY KEY, task_id TEXT NOT NULL, state TEXT NOT NULL, outcome TEXT, summary TEXT, result_ref TEXT, started_at INTEGER NOT NULL, completed_at INTEGER) WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO executions(idempotency_key,task_id,state,outcome,summary,started_at,completed_at) VALUES(?,?,?,?,?,?,?)`, "step4-legacy-0001", "et_0123456789abcdef0123456789abcdef", JournalCompleted, edge.OutcomeSucceeded, "legacy result", time.Now().Add(-time.Hour).Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	entry, err := journal.BeginAttempt("step4-legacy-0001", "et_0123456789abcdef0123456789abcdef", 4)
	if err != nil || entry.New || entry.State != JournalCompleted || entry.Attempt != 1 || entry.Result.Summary != "legacy result" || !strings.HasPrefix(entry.ResultID, "jr_") {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
}
