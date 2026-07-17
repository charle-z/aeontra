package taskjournal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestControllerActivityReportsOnlyDurableControllers(t *testing.T) {
	if activities, err := (*Journal)(nil).ControllerActivity(); err != nil || len(activities) != 0 {
		t.Fatalf("nil activities=%+v err=%v", activities, err)
	}
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 15, 0, 0, 123, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(taskID(900), "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := journal.Start(taskID(901), "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := journal.Transition(taskID(901), StateCompleted); err != nil {
		t.Fatal(err)
	}
	activities, err := journal.ControllerActivity()
	if err != nil || len(activities) != 2 {
		t.Fatalf("activities=%+v err=%v", activities, err)
	}
	if activities[0].Controller != "http" || activities[0].ActiveOperations != 1 || activities[0].LastSeenAt.IsZero() {
		t.Fatalf("http activity=%+v", activities[0])
	}
	if activities[1].Controller != "internal" || activities[1].ActiveOperations != 0 || activities[1].LastSeenAt.IsZero() {
		t.Fatalf("internal activity=%+v", activities[1])
	}
}

func TestLegacyStoreListsSortsAndDerivesStaleSnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy")
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 16, 0, 0, 0, time.UTC)
	entries := make([]Entry, 0, 3)
	for index, state := range []State{StateExecuting, StateCompleted, StateValidating} {
		entry, err := newEntry(taskID(920+index), "repo_status", "http", state, now.Add(time.Duration(index)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if isTerminal(state) {
			terminal := entry.HeartbeatAt
			entry.TerminalAt = &terminal
		}
		if err := store.Put(entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	listed, err := store.List(2)
	if err != nil || len(listed) != 2 || listed[0].TaskID != entries[2].TaskID || listed[1].TaskID != entries[1].TaskID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if _, err := store.List(0); err == nil {
		t.Fatal("zero list limit accepted")
	}
	if _, err := (*Store)(nil).List(1); err == nil {
		t.Fatal("nil store list accepted")
	}
	stale := staleSnapshot(entries, now.Add(time.Minute), 10*time.Second)
	if stale[0].State != StateDisconnected || stale[1].State != StateCompleted || stale[2].State != StateDisconnected {
		t.Fatalf("stale=%+v", stale)
	}
	if entries[0].State != StateExecuting || entries[2].State != StateValidating {
		t.Fatal("staleSnapshot mutated source")
	}
	if _, ok, err := store.Get(taskID(999)); err != nil || ok {
		t.Fatalf("missing get ok=%v err=%v", ok, err)
	}
}

func TestLegacyStoreRejectsMalformedAndUnsafeListEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy")
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(root, taskID(930)+".json")
	if err := os.WriteFile(malformed, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(10); err == nil {
		t.Fatal("malformed entry accepted")
	}
	if err := os.Remove(malformed); err != nil {
		t.Fatal(err)
	}
	entry, err := newEntry(taskID(931), "repo_status", "stdio", StateExecuting, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, body, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, entry.TaskID+".json")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := store.List(10); err == nil {
			t.Fatal("symlink list entry accepted")
		}
	}
}
