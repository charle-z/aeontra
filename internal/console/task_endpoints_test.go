package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

func newTaskConsole(t *testing.T, journal *taskjournal.Journal) *Handler {
	t.Helper()
	handler, err := New(Config{
		Runtime:     Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 78, CatalogHash: "sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed"},
		Authorize:   func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer test" },
		TaskJournal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestTasksEndpointUsesExactSafeAllowlist(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Start("0123456789abcdef0123456789abcdef", "sandbox_status", "http"); err != nil {
		t.Fatal(err)
	}
	handler := newTaskConsole(t, journal)
	request := httptest.NewRequest(http.MethodGet, tasksPath, nil)
	request.Header.Set("Authorization", "Bearer test")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(raw); strings.Join(got, ",") != "available,has_more,next_cursor,schema_version,storage,tasks" {
		t.Fatalf("top-level keys=%v", got)
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tasks"], &tasks); err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%v err=%v", tasks, err)
	}
	if got := sortedKeys(tasks[0]); strings.Join(got, ",") != "controller,created_at,derived_state,edge_id,heartbeat_at,operation,project_id,safe_summary,sequence,state,task_id,terminal_at,updated_at,version" {
		t.Fatalf("task keys=%v", got)
	}
	var storage map[string]json.RawMessage
	if err := json.Unmarshal(raw["storage"], &storage); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(storage); strings.Join(got, ",") != "database_size_bytes,detail,record_count,storage,wal_size_bytes" {
		t.Fatalf("storage keys=%v", got)
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"params", "result", "prompt", "repo", "path", "token", "ip"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("tasks payload leaked forbidden key %q: %s", forbidden, body)
		}
	}
}

func TestTasksEndpointReportsUnavailableWithoutConfiguredJournal(t *testing.T) {
	handler := newTaskConsole(t, nil)
	request := httptest.NewRequest(http.MethodGet, tasksPath, nil)
	request.Header.Set("Authorization", "Bearer test")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"available":false`) || !strings.Contains(response.Body.String(), `"tasks":[]`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTaskEventsStreamsSnapshotWithoutWebSockets(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Start("0123456789abcdef0123456789abcdef", "sandbox_status", "http"); err != nil {
		t.Fatal(err)
	}
	handler := newTaskConsole(t, journal)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, taskEventsPath, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.handleTaskEvents(response, request)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task SSE did not stop after disconnect")
	}
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), "event: snapshot") || strings.Contains(strings.ToLower(response.Body.String()), "websocket") {
		t.Fatalf("unexpected SSE body=%s", response.Body.String())
	}
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestTasksEndpointRejectsUnknownOrMalformedQuery(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	handler := newTaskConsole(t, journal)
	for _, target := range []string{
		tasksPath + "?unknown=value",
		tasksPath + "?limit=1&limit=2",
		tasksPath + "?limit=999",
		tasksPath + "?cursor=not-a-cursor",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer test")
		if got := serveConsole(t, handler, request).Code; got != http.StatusBadRequest {
			t.Fatalf("%s status=%d", target, got)
		}
	}
}
