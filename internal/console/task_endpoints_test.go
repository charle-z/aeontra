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
		Runtime:     Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 67, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
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
	if got := sortedKeys(raw); strings.Join(got, ",") != "available,schema_version,tasks" {
		t.Fatalf("top-level keys=%v", got)
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(raw["tasks"], &tasks); err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%v err=%v", tasks, err)
	}
	if got := sortedKeys(tasks[0]); strings.Join(got, ",") != "controller,heartbeat,operation,state,summary,task_id" {
		t.Fatalf("task keys=%v", got)
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
