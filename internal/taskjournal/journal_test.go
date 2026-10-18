package taskjournal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testTaskID = "0123456789abcdef0123456789abcdef"

func TestJournalPersistsTransitionsAndPublishesSafeEvents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	journal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	events, cancel := journal.Subscribe()
	defer cancel()

	if err := journal.Start(testTaskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	started := <-events
	if started.TaskID != testTaskID || started.Operation != "repo_status" || started.Task.Summary != "MCP tool operation: repo_status" || started.State != StateExecuting {
		t.Fatalf("started=%+v", started)
	}
	now = now.Add(time.Second)
	if err := journal.Transition(testTaskID, StateValidating); err != nil {
		t.Fatal(err)
	}
	if got := <-events; got.State != StateValidating || !got.Task.HeartbeatAt.Equal(now) {
		t.Fatalf("validating=%+v", got)
	}
	now = now.Add(time.Second)
	if err := journal.Transition(testTaskID, StateCompleted); err != nil {
		t.Fatal(err)
	}
	if got := <-events; got.State != StateCompleted {
		t.Fatalf("completed=%+v", got)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now }
	snapshot, err := reopened.Snapshot(10)
	if err != nil || len(snapshot) != 1 || snapshot[0].State != StateCompleted {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"params", "result", "prompt", "path", "token", "identity"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("entry leaked %q: %s", forbidden, body)
		}
	}
}

func TestJournalReportsStaleActiveTaskAsDisconnected(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	journal.staleAfter = 10 * time.Second
	if err := journal.Start(testTaskID, "run_tests", "stdio"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	snapshot, err := journal.Snapshot(1)
	if err != nil || snapshot[0].State != StateDisconnected {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	entry, ok, err := journal.store.Get(testTaskID)
	if err != nil || !ok || entry.State != StateExecuting {
		t.Fatalf("durable entry=%+v ok=%v err=%v", entry, ok, err)
	}
}

func TestJournalHeartbeatPreventsFalseDisconnectAndTerminalIsImmutable(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	journal.staleAfter = 10 * time.Second
	if err := journal.Start(testTaskID, "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Second)
	if err := journal.Heartbeat(testTaskID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(9 * time.Second)
	snapshot, err := journal.Snapshot(1)
	if err != nil || snapshot[0].State != StateExecuting {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if err := journal.Transition(testTaskID, StateFailed); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition(testTaskID, StateCompleted); err == nil {
		t.Fatal("terminal transition should fail")
	}
	if err := journal.Heartbeat(testTaskID); err != nil {
		t.Fatalf("terminal heartbeat should be harmless: %v", err)
	}
}

func TestJournalRejectsUnsafeFieldsAndFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	journal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ taskID, operation, controller string }{
		{"../escape", "repo_status", "http"},
		{testTaskID, "read/file", "http"},
		{testTaskID, "repo_status", "human-name"},
	} {
		if err := journal.Start(test.taskID, test.operation, test.controller); err == nil {
			t.Fatalf("unsafe journal entry accepted: %+v", test)
		}
	}
	if _, err := Open("relative/tasks"); err == nil {
		t.Fatal("relative root accepted")
	}
	symlinkRoot := filepath.Join(t.TempDir(), "symlink-tasks")
	if err := os.MkdirAll(symlinkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "target.db"), filepath.Join(symlinkRoot, "tasks.db")); err == nil {
		if _, err := Open(symlinkRoot); err == nil {
			t.Fatal("symlink database accepted")
		}
	}
	if err := journal.Transition("ffffffffffffffffffffffffffffffff", StateCompleted); err == nil {
		t.Fatal("missing task transition accepted")
	}
	if _, err := journal.Snapshot(MaxPageSize + 1); err == nil {
		t.Fatal("invalid snapshot limit accepted")
	}
}
