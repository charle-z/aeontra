package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

func TestEventLogEndpointPaginatesFiltersAndUsesClosedSchema(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.Start("0123456789abcdef0123456789abcdef", "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition("0123456789abcdef0123456789abcdef", taskjournal.StateCompleted); err != nil {
		t.Fatal(err)
	}
	if err := journal.Start("1123456789abcdef0123456789abcdef", "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition("1123456789abcdef0123456789abcdef", taskjournal.StateFailed); err != nil {
		t.Fatal(err)
	}
	handler := newTaskConsole(t, journal)

	request := httptest.NewRequest(http.MethodGet, eventLogPath+"?limit=1&controller=internal&state=failed&operation=run_tests&event_type=transition", nil)
	request.Header.Set("Authorization", "Bearer test")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedKeys(raw), ","); got != "available,events,has_more,next_cursor,schema_version,storage" {
		t.Fatalf("keys=%s", got)
	}
	var events []map[string]json.RawMessage
	if err := json.Unmarshal(raw["events"], &events); err != nil || len(events) != 1 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	if got := strings.Join(sortedKeys(events[0]), ","); got != "event_id,event_type,occurred_at,operation,sequence,state,task,task_id,task_version" {
		t.Fatalf("event keys=%s", got)
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"prompt", "result", "path", "repo", "token", "identity", "parameter"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("event log leaked %q: %s", forbidden, body)
		}
	}
	for _, target := range []string{
		eventLogPath + "?unknown=value",
		eventLogPath + "?controller=owner",
		eventLogPath + "?state=unknown",
		eventLogPath + "?operation=../bad",
		eventLogPath + "?event_type=private",
		eventLogPath + "?limit=999",
	} {
		bad := httptest.NewRequest(http.MethodGet, target, nil)
		bad.Header.Set("Authorization", "Bearer test")
		if got := serveConsole(t, handler, bad).Code; got != http.StatusBadRequest {
			t.Fatalf("%s status=%d", target, got)
		}
	}
}

func TestSSEReplaysLastEventIDAndResetsOnGap(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err := journal.Start("0123456789abcdef0123456789abcdef", "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition("0123456789abcdef0123456789abcdef", taskjournal.StateCompleted); err != nil {
		t.Fatal(err)
	}
	handler := newTaskConsole(t, journal)

	serve := func(target, headerID string) *httptest.ResponseRecorder {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		request.Header.Set("Authorization", "Bearer test")
		if headerID != "" {
			request.Header.Set("Last-Event-ID", headerID)
		}
		response := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { handler.handleTaskEvents(response, request); close(done) }()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SSE did not stop")
		}
		return response
	}

	for _, replay := range []*httptest.ResponseRecorder{
		serve(taskEventsPath, "1"),
		serve(taskEventsPath+"?last_event_id=1", ""),
	} {
		body := replay.Body.String()
		if !strings.Contains(body, "retry: 2000") || !strings.Contains(body, "id: 2\nevent: journal") || strings.Contains(body, "event: snapshot") {
			t.Fatalf("unexpected replay=%s", body)
		}
	}
	reset := serve(taskEventsPath+"?last_event_id=9999", "")
	if body := reset.Body.String(); !strings.Contains(body, "event: snapshot") || !strings.Contains(body, "id: 2\nevent: event_snapshot") || !strings.Contains(body, "event: stream") {
		t.Fatalf("gap did not reset snapshots: %s", body)
	}

	conflict := httptest.NewRequest(http.MethodGet, taskEventsPath+"?last_event_id=2", nil)
	conflict.Header.Set("Authorization", "Bearer test")
	conflict.Header.Set("Last-Event-ID", "1")
	if got := serveConsole(t, handler, conflict).Code; got != http.StatusBadRequest {
		t.Fatalf("conflicting Last-Event-ID status=%d", got)
	}
}
