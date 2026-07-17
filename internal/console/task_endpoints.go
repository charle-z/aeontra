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
	response := tasksResponse{
		SchemaVersion: 2,
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
	page, err := h.taskJournal.Page(limit, cursor)
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
	limit := taskjournal.DefaultPageSize
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > taskjournal.MaxPageSize {
			writeGenericError(w, http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	response, err := h.taskSnapshot(limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeGenericError(w, http.StatusServiceUnavailable)
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
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
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

	if after > 0 {
		events, gap, err := h.taskJournal.Replay(after, taskjournal.MaxPageSize)
		if err != nil {
			return
		}
		if gap {
			snapshot, err := h.taskSnapshot(taskjournal.DefaultPageSize, "")
			if err != nil || !writeEvent(0, "snapshot", snapshot) {
				return
			}
		} else {
			for _, event := range events {
				if !writeEvent(event.EventID, "task", event) {
					return
				}
				after = event.EventID
			}
		}
	} else {
		snapshot, err := h.taskSnapshot(taskjournal.DefaultPageSize, "")
		if err != nil || !writeEvent(0, "snapshot", snapshot) {
			return
		}
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
			if !writeEvent(event.EventID, "task", event) {
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
