package console

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

const (
	tasksPath      = "/console/tasks"
	taskEventsPath = "/console/events"
)

type tasksResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	Available     bool                `json:"available"`
	Storage       taskjournal.Status  `json:"storage"`
	Tasks         []taskjournal.Entry `json:"tasks"`
	NextCursor    string              `json:"next_cursor"`
	HasMore       bool                `json:"has_more"`
}

func (h *Handler) taskSnapshot(limit int, cursor string) (tasksResponse, error) {
	return h.taskSnapshotFiltered(limit, cursor, taskjournal.TaskFilter{})
}

func (h *Handler) taskSnapshotFiltered(limit int, cursor string, filter taskjournal.TaskFilter) (tasksResponse, error) {
	response := tasksResponse{
		SchemaVersion: 3,
		Available:     h.taskJournal != nil,
		Storage: taskjournal.Status{
			Storage: taskjournal.StorageDegraded,
			Detail:  "journal unavailable",
		},
		Tasks: []taskjournal.Entry{},
	}
	if h.taskJournal == nil {
		return response, nil
	}
	page, err := h.taskJournal.PageFiltered(limit, cursor, filter)
	if err != nil {
		return tasksResponse{}, err
	}
	response.Storage = h.taskJournal.Status()
	response.Tasks = page.Entries
	response.NextCursor = page.NextCursor
	response.HasMore = page.HasMore
	return response, nil
}

func (h *Handler) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	allowed := map[string]struct{}{
		"limit": {}, "cursor": {}, "controller": {}, "state": {}, "operation": {}, "project_id": {}, "edge_id": {},
	}
	for key, values := range r.URL.Query() {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
	}
	limit := taskjournal.DefaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > taskjournal.MaxPageSize {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	filter := taskjournal.TaskFilter{
		Controller: strings.TrimSpace(r.URL.Query().Get("controller")),
		State:      taskjournal.State(strings.TrimSpace(r.URL.Query().Get("state"))),
		Operation:  strings.TrimSpace(r.URL.Query().Get("operation")),
		ProjectID:  strings.TrimSpace(r.URL.Query().Get("project_id")),
		EdgeID:     strings.TrimSpace(r.URL.Query().Get("edge_id")),
	}
	response, err := h.taskSnapshotFiltered(limit, strings.TrimSpace(r.URL.Query().Get("cursor")), filter)
	if err != nil {
		writeGenericError(w, http.StatusBadRequest)
		return
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *Handler) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	if h.taskJournal == nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	after := int64(0)
	headerID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	queryID := strings.TrimSpace(r.URL.Query().Get("last_event_id"))
	for key, values := range r.URL.Query() {
		if key != "last_event_id" || len(values) != 1 {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
	}
	if headerID != "" && queryID != "" && headerID != queryID {
		writeGenericError(w, http.StatusBadRequest)
		return
	}
	rawID := headerID
	if rawID == "" {
		rawID = queryID
	}
	if rawID != "" {
		parsed, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || parsed < 0 {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		after = parsed
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, "retry: 2000\n\n"); err != nil {
		return
	}
	flusher.Flush()

	writeEvent := func(id int64, name string, value any) bool {
		body, err := json.Marshal(value)
		if err != nil {
			return false
		}
		if id > 0 {
			if _, err := io.WriteString(w, "id: "+strconv.FormatInt(id, 10)+"\n"); err != nil {
				return false
			}
		}
		if _, err := io.WriteString(w, "event: "+name+"\ndata: "+string(body)+"\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	writeJournalEvent := func(event taskjournal.Event) bool {
		if event.EventID <= 0 {
			return false
		}
		return writeEvent(event.EventID, "journal", event)
	}

	writeSnapshots := func() bool {
		tasks, err := h.taskSnapshot(taskjournal.DefaultPageSize, "")
		if err != nil || !writeEvent(0, "snapshot", tasks) {
			return false
		}
		events, err := h.eventSnapshot(taskjournal.DefaultPageSize, "", taskjournal.EventFilter{})
		if err != nil {
			return false
		}
		latestEventID := int64(0)
		if len(events.Events) > 0 {
			latestEventID = events.Events[0].EventID
		}
		return writeEvent(latestEventID, "event_snapshot", events)
	}
	if after > 0 {
		events, gap, err := h.taskJournal.Replay(after, taskjournal.MaxPageSize)
		if err != nil {
			return
		}
		if gap {
			if !writeSnapshots() {
				return
			}
		} else {
			for _, event := range events {
				if !writeJournalEvent(event) {
					return
				}
				after = event.EventID
			}
		}
	} else if !writeSnapshots() {
		return
	}
	if !writeEvent(0, "stream", map[string]string{"state": "live"}) {
		return
	}

	updates, cancel := h.taskJournal.Subscribe()
	defer cancel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-updates:
			if !open {
				return
			}
			if event.EventID <= after {
				continue
			}
			if !writeJournalEvent(event) {
				return
			}
			after = event.EventID
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
