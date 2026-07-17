package console

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/taskjournal"
)

const eventLogPath = "/console/event-log"

type eventLogResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	Available     bool                `json:"available"`
	Storage       taskjournal.Status  `json:"storage"`
	Events        []taskjournal.Event `json:"events"`
	NextCursor    string              `json:"next_cursor"`
	HasMore       bool                `json:"has_more"`
}

func (h *Handler) eventSnapshot(limit int, cursor string, filter taskjournal.EventFilter) (eventLogResponse, error) {
	response := eventLogResponse{
		SchemaVersion: 1,
		Available:     h.taskJournal != nil,
		Storage: taskjournal.Status{
			Storage: taskjournal.StorageDegraded,
			Detail:  "journal unavailable",
		},
		Events: []taskjournal.Event{},
	}
	if h.taskJournal == nil {
		return response, nil
	}
	page, err := h.taskJournal.EventPage(limit, cursor, filter)
	if err != nil {
		return eventLogResponse{}, err
	}
	response.Storage = h.taskJournal.Status()
	response.Events = page.Events
	response.NextCursor = page.NextCursor
	response.HasMore = page.HasMore
	return response, nil
}

func (h *Handler) handleEventLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	allowed := map[string]struct{}{
		"limit": {}, "cursor": {}, "controller": {}, "state": {}, "operation": {}, "event_type": {}, "project_id": {}, "edge_id": {},
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
	filter := taskjournal.EventFilter{
		Controller: strings.TrimSpace(r.URL.Query().Get("controller")),
		State:      taskjournal.State(strings.TrimSpace(r.URL.Query().Get("state"))),
		Operation:  strings.TrimSpace(r.URL.Query().Get("operation")),
		EventType:  strings.TrimSpace(r.URL.Query().Get("event_type")),
		ProjectID:  strings.TrimSpace(r.URL.Query().Get("project_id")),
		EdgeID:     strings.TrimSpace(r.URL.Query().Get("edge_id")),
	}
	response, err := h.eventSnapshot(limit, strings.TrimSpace(r.URL.Query().Get("cursor")), filter)
	if err != nil {
		writeGenericError(w, http.StatusBadRequest)
		return
	}
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}
