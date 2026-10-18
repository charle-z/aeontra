package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

type parsedSSEFrame struct {
	ID    int64
	Event string
	Data  string
}

func TestSSEJournalFramesCarryPhysicalIDsAndReplayMonotonically(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	first := "0123456789abcdef0123456789abcdef"
	second := "1123456789abcdef0123456789abcdef"
	if err := journal.Start(first, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition(first, taskjournal.StateCompleted); err != nil {
		t.Fatal(err)
	}
	if err := journal.Start(second, "run_tests", "internal"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Heartbeat(second); err != nil {
		t.Fatal(err)
	}

	handler := newTaskConsole(t, journal)
	replay := serveSSEHotfix(t, handler, taskEventsPath, "1")
	if !strings.HasPrefix(replay, "retry: 2000\n\n") {
		t.Fatalf("retry frame does not use physical newlines: %q", replay)
	}
	frames := parseSSEHotfix(t, replay)
	journalFrames := filterSSEHotfix(frames, "journal")
	if len(journalFrames) != 3 {
		t.Fatalf("journal frame count=%d, want 3: %q", len(journalFrames), replay)
	}
	seen := map[string]struct{}{}
	previous := int64(1)
	for _, frame := range journalFrames {
		if frame.ID <= previous {
			t.Fatalf("non-monotonic journal id=%d after %d", frame.ID, previous)
		}
		previous = frame.ID
		var event taskjournal.Event
		if err := json.Unmarshal([]byte(frame.Data), &event); err != nil {
			t.Fatalf("decoding journal frame: %v", err)
		}
		if event.EventID != frame.ID {
			t.Fatalf("frame id=%d event id=%d", frame.ID, event.EventID)
		}
		key := strconv.FormatInt(event.EventID, 10) + ":" + strconv.FormatInt(event.TaskVersion, 10)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate journal event %s", key)
		}
		seen[key] = struct{}{}
	}
	if previous != 4 {
		t.Fatalf("last replay id=%d, want 4", previous)
	}

	fallback := serveSSEHotfix(t, handler, taskEventsPath+"?last_event_id=1", "")
	fallbackFrames := filterSSEHotfix(parseSSEHotfix(t, fallback), "journal")
	if got := frameIDsHotfix(fallbackFrames); got != "2,3,4" {
		t.Fatalf("query fallback ids=%s", got)
	}
}

func TestSSEReconnectHasNoDuplicatesAndGapSnapshotSetsLatestID(t *testing.T) {
	journal, err := taskjournal.Open(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	taskID := "0123456789abcdef0123456789abcdef"
	if err := journal.Start(taskID, "repo_status", "http"); err != nil {
		t.Fatal(err)
	}
	if err := journal.Heartbeat(taskID); err != nil {
		t.Fatal(err)
	}
	if err := journal.Transition(taskID, taskjournal.StateCompleted); err != nil {
		t.Fatal(err)
	}

	handler := newTaskConsole(t, journal)
	initial := parseSSEHotfix(t, serveSSEHotfix(t, handler, taskEventsPath, ""))
	snapshot := filterSSEHotfix(initial, "event_snapshot")
	if len(snapshot) != 1 || snapshot[0].ID != 3 {
		t.Fatalf("initial snapshot id=%v, want 3", frameIDsHotfix(snapshot))
	}

	reconnect := parseSSEHotfix(t, serveSSEHotfix(t, handler, taskEventsPath, "3"))
	if frames := filterSSEHotfix(reconnect, "journal"); len(frames) != 0 {
		t.Fatalf("reconnect replayed duplicates: %s", frameIDsHotfix(frames))
	}
	if len(filterSSEHotfix(reconnect, "stream")) != 1 {
		t.Fatalf("reconnect did not reach live stream")
	}

	gap := parseSSEHotfix(t, serveSSEHotfix(t, handler, taskEventsPath+"?last_event_id=9999", ""))
	if len(filterSSEHotfix(gap, "snapshot")) != 1 {
		t.Fatalf("gap did not emit task snapshot")
	}
	gapSnapshot := filterSSEHotfix(gap, "event_snapshot")
	if len(gapSnapshot) != 1 || gapSnapshot[0].ID != 3 {
		t.Fatalf("gap snapshot id=%s, want 3", frameIDsHotfix(gapSnapshot))
	}
	if len(filterSSEHotfix(gap, "journal")) != 0 {
		t.Fatalf("gap reset must not replay stale journal frames")
	}
}

func serveSSEHotfix(t *testing.T, handler *Handler, target, headerID string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer test")
	if headerID != "" {
		request.Header.Set("Last-Event-ID", headerID)
	}
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
		t.Fatal("SSE did not stop")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("SSE status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func parseSSEHotfix(t *testing.T, body string) []parsedSSEFrame {
	t.Helper()
	parts := strings.Split(body, "\n\n")
	frames := make([]parsedSSEFrame, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "retry:") || strings.HasPrefix(part, ":") {
			continue
		}
		lines := strings.Split(part, "\n")
		frame := parsedSSEFrame{}
		for _, line := range lines {
			switch {
			case strings.HasPrefix(line, "id: "):
				value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "id: ")), 10, 64)
				if err != nil || value <= 0 {
					t.Fatalf("invalid SSE id line %q", line)
				}
				frame.ID = value
			case strings.HasPrefix(line, "event: "):
				frame.Event = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			case strings.HasPrefix(line, "data: "):
				frame.Data = strings.TrimPrefix(line, "data: ")
			default:
				t.Fatalf("unexpected SSE line %q", line)
			}
		}
		if frame.Event == "journal" {
			if len(lines) != 3 || frame.ID <= 0 || frame.Data == "" || !strings.HasPrefix(lines[0], "id: ") || lines[1] != "event: journal" || !strings.HasPrefix(lines[2], "data: ") {
				t.Fatalf("journal frame is not exact id/event/data: %q", part)
			}
		}
		frames = append(frames, frame)
	}
	return frames
}

func filterSSEHotfix(frames []parsedSSEFrame, event string) []parsedSSEFrame {
	filtered := make([]parsedSSEFrame, 0)
	for _, frame := range frames {
		if frame.Event == event {
			filtered = append(filtered, frame)
		}
	}
	return filtered
}

func frameIDsHotfix(frames []parsedSSEFrame) string {
	values := make([]string, 0, len(frames))
	for _, frame := range frames {
		values = append(values, strconv.FormatInt(frame.ID, 10))
	}
	return strings.Join(values, ",")
}
