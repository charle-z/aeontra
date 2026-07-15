package console

import (
	"encoding/json"
	"io"
	"net/http"
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
	Tasks         []taskjournal.Entry `json:"tasks"`
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
	response := tasksResponse{SchemaVersion: 1, Available: h.taskJournal != nil, Tasks: []taskjournal.Entry{}}
	if h.taskJournal != nil {
		tasks, err := h.taskJournal.Snapshot(100)
		if err != nil {
			writeGenericError(w, http.StatusServiceUnavailable)
			return
		}
		response.Tasks = tasks
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
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(name string, value any) bool {
		body, err := json.Marshal(value)
		if err != nil {
			return false
		}
		if _, err := io.WriteString(w, "event: "+name+"\ndata: "+string(body)+"\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	snapshot, err := h.taskJournal.Snapshot(100)
	if err != nil || !writeEvent("snapshot", tasksResponse{SchemaVersion: 1, Available: true, Tasks: snapshot}) {
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
		case entry, open := <-updates:
			if !open || !writeEvent("task", entry) {
				return
			}
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
