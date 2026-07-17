package taskjournal

import (
	"testing"
	"time"
)

func TestSQLiteBytePruneRunsBeforeHardCapAndPreservesActiveTasks(t *testing.T) {
	journal, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	now := time.Date(2026, 7, 17, 15, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(testTaskID, "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}

	tx, err := journal.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO tasks(task_id,controller,operation,safe_summary,state,created_at,updated_at,heartbeat_at,terminal_at,version) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 700; index++ {
		stamp := now.Add(-time.Duration(index+1) * time.Second)
		if _, err := statement.Exec(taskID(index+1000), "http", "repo_status", "MCP tool operation: repo_status", StateCompleted,
			unixNano(stamp), unixNano(stamp), unixNano(stamp), unixNano(stamp), 1); err != nil {
			t.Fatal(err)
		}
	}
	_ = statement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	usedBefore, err := journal.store.sqliteUsedBytesLocked()
	if err != nil {
		t.Fatal(err)
	}
	var pageSize int64
	if err := journal.store.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	target := usedBefore - 2*pageSize
	if target <= 0 {
		t.Fatalf("fixture is too small: used=%d page=%d", usedBefore, pageSize)
	}
	if err := journal.store.pruneBytesLocked(target); err != nil {
		t.Fatal(err)
	}
	usedAfter, err := journal.store.sqliteUsedBytesLocked()
	if err != nil {
		t.Fatal(err)
	}
	if usedAfter > target || usedAfter >= usedBefore {
		t.Fatalf("byte prune did not reduce used pages: before=%d after=%d target=%d", usedBefore, usedAfter, target)
	}
	if _, ok, err := journal.store.Get(testTaskID); err != nil || !ok {
		t.Fatalf("active task was removed: ok=%v err=%v", ok, err)
	}
	var terminalCount int
	if err := journal.store.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE terminal_at IS NOT NULL`).Scan(&terminalCount); err != nil {
		t.Fatal(err)
	}
	if terminalCount >= 700 {
		t.Fatalf("terminal tasks were not pruned: count=%d", terminalCount)
	}
	if err := journal.store.pruneBytesLocked(0); err == nil {
		t.Fatal("invalid byte target was accepted")
	}
}
