package taskjournal

import (
	"testing"
	"time"
)

func TestEventPagesUseStableCursorAndExactFilters(t *testing.T) {
	journal, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	for index := 0; index < 30; index++ {
		controller := "http"
		operation := "repo_status"
		state := StateCompleted
		if index%2 == 1 {
			controller = "internal"
			operation = "run_tests"
			state = StateFailed
		}
		if err := journal.Start(taskID(index), operation, controller); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
		if err := journal.Transition(taskID(index), state); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
	}

	seen := map[int64]struct{}{}
	cursor := ""
	for {
		page, err := journal.EventPage(7, cursor, EventFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			if _, duplicate := seen[event.EventID]; duplicate {
				t.Fatalf("duplicate event %d", event.EventID)
			}
			seen[event.EventID] = struct{}{}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			t.Fatalf("cursor did not advance: %q", page.NextCursor)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 60 {
		t.Fatalf("events=%d want=60", len(seen))
	}

	filtered, err := journal.EventPage(100, "", EventFilter{Controller: "internal", State: StateFailed, Operation: "run_tests", EventType: EventTransition})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Events) != 15 {
		t.Fatalf("filtered events=%d want=15", len(filtered.Events))
	}
	for _, event := range filtered.Events {
		if event.Task.Controller != "internal" || event.State != StateFailed || event.Operation != "run_tests" || event.EventType != EventTransition {
			t.Fatalf("filter mismatch: %+v", event)
		}
	}
	for _, invalid := range []EventFilter{
		{Controller: "owner"}, {State: "unknown"}, {Operation: "../bad"}, {EventType: "private"},
	} {
		if _, err := journal.EventPage(10, "", invalid); err == nil {
			t.Fatalf("invalid filter accepted: %+v", invalid)
		}
	}
	if _, err := journal.EventPage(10, "not-a-cursor", EventFilter{}); err == nil {
		t.Fatal("invalid event cursor accepted")
	}
}

func TestEventRetentionAndQuotaAreDurable(t *testing.T) {
	journal, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	now := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return now }
	if err := journal.Start(testTaskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.store.db.Exec(`UPDATE task_events SET occurred_at=? WHERE event_id=1`, unixNano(now.Add(-EventRetention-time.Second))); err != nil {
		t.Fatal(err)
	}
	if err := journal.store.Prune(now); err != nil {
		t.Fatal(err)
	}
	var oldCount int
	if err := journal.store.db.QueryRow(`SELECT COUNT(*) FROM task_events WHERE event_id=1`).Scan(&oldCount); err != nil || oldCount != 0 {
		t.Fatalf("old event count=%d err=%v", oldCount, err)
	}

	tx, err := journal.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO task_events(task_id,task_version,sequence,occurred_at,event_type,state,operation) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxEvents+17; index++ {
		if _, err := statement.Exec(testTaskID, 1, 1, unixNano(now.Add(time.Duration(index)*time.Nanosecond)), EventHeartbeat, StateExecuting, "repo_status"); err != nil {
			t.Fatal(err)
		}
	}
	_ = statement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := journal.store.Prune(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := journal.store.db.QueryRow(`SELECT COUNT(*) FROM task_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != MaxEvents {
		t.Fatalf("event count=%d want=%d", count, MaxEvents)
	}
	page, err := journal.EventPage(1, "", EventFilter{})
	if err != nil || len(page.Events) != 1 || page.Events[0].EventID <= int64(MaxEvents) {
		t.Fatalf("newest event missing: page=%+v err=%v", page, err)
	}
	if MaxEvents != 20_000 || EventRetention != 30*24*time.Hour {
		t.Fatalf("unexpected retention contract: max=%d retention=%s", MaxEvents, EventRetention)
	}
}
