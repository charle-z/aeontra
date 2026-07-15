package taskjournal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJournalRejectsDuplicateStartInvalidStateAndMissingHeartbeat(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Start(testTaskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Start(testTaskID, "run_tests", "stdio"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate err=%v", err)
	}
	if err := journal.Transition(testTaskID, State("invented")); err == nil {
		t.Fatal("invalid transition state accepted")
	}
	entry := Entry{TaskID: testTaskID, Operation: "repo_status", Summary: "MCP tool operation: repo_status", State: StateExecuting, Controller: "http"}
	if err := entry.validate(); err == nil {
		t.Fatal("zero heartbeat accepted")
	}
	entry.Heartbeat = time.Now()
	entry.Summary = "private free-form text"
	if err := entry.validate(); err == nil {
		t.Fatal("free-form summary accepted")
	}
}

func TestStoreRejectsMalformedUnknownOversizedAndUnsafeEntryFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, testTaskID+".json")
	for _, body := range []string{
		`{"task_id":"0123456789abcdef0123456789abcdef","operation":"repo_status","summary":"MCP tool operation: repo_status","state":"executing","heartbeat":"2026-07-14T20:00:00Z","controller":"http","extra":"no"}`,
		`{"task_id":`,
		strings.Repeat("x", maxEntryFileSize+1),
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Get(testTaskID); err == nil {
			t.Fatalf("unsafe body accepted: %.32q", body)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(testTaskID); err == nil {
		t.Fatal("directory entry accepted")
	}
}

func TestJournalNilPathsAndSubscriberCancellationAreSafe(t *testing.T) {
	var journal *Journal
	if err := journal.Start(testTaskID, "repo_status", "http"); err == nil {
		t.Fatal("nil journal start succeeded")
	}
	if err := journal.Transition(testTaskID, StateCompleted); err == nil {
		t.Fatal("nil journal transition succeeded")
	}
	if err := journal.Heartbeat(testTaskID); err == nil {
		t.Fatal("nil journal heartbeat succeeded")
	}
	if _, err := journal.Snapshot(1); err == nil {
		t.Fatal("nil journal snapshot succeeded")
	}
	updates, cancel := journal.Subscribe()
	if _, open := <-updates; open {
		t.Fatal("nil journal subscription remained open")
	}
	cancel()

	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	updates, cancel = journal.Subscribe()
	cancel()
	cancel()
	if _, open := <-updates; open {
		t.Fatal("cancelled subscription remained open")
	}
}

func TestStoreEntryLimitAndPrivateModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	store, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("root mode=%v err=%v", rootInfo.Mode(), err)
	}
	now := time.Now().UTC()
	for index := 0; index < maxEntries; index++ {
		taskID := strings.Repeat("0", 24) + strings.ToLower(hexEight(index))
		entry, createErr := newEntry(taskID, "repo_status", "internal", StateExecuting, now)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if err := store.Put(entry); err != nil {
			t.Fatalf("put %d: %v", index, err)
		}
	}
	extra, err := newEntry("ffffffffffffffffffffffffffffffff", "repo_status", "internal", StateExecuting, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(extra); err == nil {
		t.Fatal("entry cap not enforced")
	}
	info, err := os.Stat(filepath.Join(root, strings.Repeat("0", 32)+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("entry mode=%v err=%v", info.Mode(), err)
	}
}

func hexEight(value int) string {
	const alphabet = "0123456789abcdef"
	buffer := make([]byte, 8)
	for index := len(buffer) - 1; index >= 0; index-- {
		buffer[index] = alphabet[value&15]
		value >>= 4
	}
	return string(buffer)
}
