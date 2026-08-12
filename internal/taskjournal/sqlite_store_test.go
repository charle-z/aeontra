package taskjournal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func taskID(index int) string { return fmt.Sprintf("%032x", index+1) }

func TestSQLiteJournalAcceptsHundredsAndPaginatesWithoutDuplicates(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	for index := 0; index < 650; index++ {
		if err := journal.Start(taskID(index), "repo_status", "http"); err != nil {
			t.Fatalf("start %d: %v", index, err)
		}
		now = now.Add(time.Millisecond)
		if err := journal.Transition(taskID(index), StateCompleted); err != nil {
			t.Fatalf("complete %d: %v", index, err)
		}
		now = now.Add(time.Millisecond)
	}
	seen := make(map[string]struct{})
	cursor := ""
	for {
		page, err := journal.Page(73, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Entries {
			if _, duplicate := seen[entry.TaskID]; duplicate {
				t.Fatalf("duplicate task %s", entry.TaskID)
			}
			seen[entry.TaskID] = struct{}{}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			t.Fatalf("invalid cursor progression %q", page.NextCursor)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 650 {
		t.Fatalf("seen=%d", len(seen))
	}
	first, err := journal.Page(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Entries[0].TaskID != taskID(649) || first.Entries[1].TaskID != taskID(648) {
		t.Fatalf("newest-first order=%v", []string{first.Entries[0].TaskID, first.Entries[1].TaskID})
	}
}

func TestSQLiteJournalMigratesLegacyJSONIdempotently(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	legacy := legacyEntry{TaskID: testTaskID, Operation: "run_tests", Summary: "MCP tool operation: run_tests", State: StateCompleted, Heartbeat: legacyTime, Controller: "internal"}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(root, testTaskID+".json")
	if err := os.WriteFile(legacyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := journal.Snapshot(10)
	if err != nil || len(entries) != 1 || entries[0].TaskID != testTaskID || entries[0].State != StateCompleted {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	eventPage, err := journal.EventPage(10, "", EventFilter{})
	if err != nil || len(eventPage.Events) != 1 || eventPage.Events[0].TaskID != testTaskID || eventPage.Events[0].EventType != EventTransition || eventPage.Events[0].State != StateCompleted {
		t.Fatalf("migration events=%+v err=%v", eventPage.Events, err)
	}
	firstEventID := eventPage.Events[0].EventID
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err = reopened.Snapshot(10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("idempotent entries=%+v err=%v", entries, err)
	}
	eventPage, err = reopened.EventPage(10, "", EventFilter{})
	if err != nil || len(eventPage.Events) != 1 || eventPage.Events[0].EventID != firstEventID {
		t.Fatalf("idempotent events=%+v err=%v", eventPage.Events, err)
	}
	if _, err := os.Stat(filepath.Join(root, "legacy-archive", filepath.Base(legacyPath))); err != nil {
		t.Fatalf("legacy archive: %v", err)
	}
}

func TestSQLiteJournalReplayAndGapRecovery(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(testTaskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := journal.Transition(testTaskID, StateCompleted); err != nil {
		t.Fatal(err)
	}
	events, gap, err := journal.Replay(1, 20)
	if err != nil || gap || len(events) != 1 || events[0].EventID != 2 || events[0].TaskVersion != 2 {
		t.Fatalf("events=%+v gap=%v err=%v", events, gap, err)
	}
	if _, gap, err := journal.Replay(9999, 20); err != nil || !gap {
		t.Fatalf("future gap=%v err=%v", gap, err)
	}
}

func TestSQLiteJournalRetentionNeverDeletesActiveOperations(t *testing.T) {
	journal, err := Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(testTaskID, "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-TerminalRetention - time.Hour)
	entry, err := newEntry(taskID(2), "repo_status", "http", StateCompleted, old)
	if err != nil {
		t.Fatal(err)
	}
	entry.TerminalAt = &old
	if _, _, err := journal.store.Create(entry); err != nil {
		t.Fatal(err)
	}
	if err := journal.store.Prune(now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := journal.store.Get(testTaskID); err != nil || !ok {
		t.Fatalf("active operation removed ok=%v err=%v", ok, err)
	}
	if _, ok, err := journal.store.Get(taskID(2)); err != nil || ok {
		t.Fatalf("expired terminal retained ok=%v err=%v", ok, err)
	}
	status := journal.Status()
	if status.Storage != StorageHealthy || status.RecordCount != 1 || status.DatabaseSize <= 0 {
		t.Fatalf("status=%+v", status)
	}
}

func TestSQLiteJournalDatabaseBudgetAndPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	journal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	info, err := os.Stat(filepath.Join(root, "tasks.db"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("db mode=%v err=%v", info.Mode().Perm(), err)
	}
	dirInfo, err := os.Stat(root)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode=%v err=%v", dirInfo.Mode().Perm(), err)
	}
	var pageSize, maxPages int64
	if err := journal.store.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := journal.store.db.QueryRow(`PRAGMA max_page_count`).Scan(&maxPages); err != nil {
		t.Fatal(err)
	}
	if pageSize*maxPages > TargetMaxBytes || pageSize*maxPages < TargetMaxBytes-(1<<20) {
		t.Fatalf("database cap=%d", pageSize*maxPages)
	}
}

func TestSQLiteJournalEventsSurviveRestartWithStableCursor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	journal, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 6, 0, 0, 123456789, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(testTaskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := journal.Transition(testTaskID, StateCompleted); err != nil {
		t.Fatal(err)
	}
	first, err := journal.EventPage(1, "", EventFilter{})
	if err != nil || len(first.Events) != 1 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	second, err := reopened.EventPage(1, first.NextCursor, EventFilter{})
	if err != nil || len(second.Events) != 1 || second.Events[0].EventID == first.Events[0].EventID || second.HasMore {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if second.Events[0].OccurredAt.Format(time.RFC3339Nano) != "2026-07-17T06:00:00.123456789Z" {
		t.Fatalf("timestamp=%s", second.Events[0].OccurredAt.Format(time.RFC3339Nano))
	}
}
