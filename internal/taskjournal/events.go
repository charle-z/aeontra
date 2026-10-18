package taskjournal

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

type EventFilter struct {
	Controller string
	State      State
	Operation  string
	EventType  string
	ProjectID  string
	EdgeID     string
}

type EventPage struct {
	Events     []Event
	NextCursor string
	HasMore    bool
}

func (filter EventFilter) validate() error {
	if filter.Controller != "" {
		if _, ok := validControllers[filter.Controller]; !ok {
			return errors.New("task journal: invalid event controller filter")
		}
	}
	if filter.State != "" {
		if _, ok := validStates[filter.State]; !ok {
			return errors.New("task journal: invalid event state filter")
		}
	}
	if filter.Operation != "" && !operationPattern.MatchString(filter.Operation) {
		return errors.New("task journal: invalid event operation filter")
	}
	if filter.EventType != "" {
		switch filter.EventType {
		case EventStarted, EventHeartbeat, EventTransition:
		default:
			return errors.New("task journal: invalid event type filter")
		}
	}
	if filter.ProjectID != "" && (!scopeIDPattern.MatchString(filter.ProjectID) || !strings.HasPrefix(filter.ProjectID, "prj_")) {
		return errors.New("task journal: invalid event project filter")
	}
	if filter.EdgeID != "" && (!scopeIDPattern.MatchString(filter.EdgeID) || !strings.HasPrefix(filter.EdgeID, "edge_")) {
		return errors.New("task journal: invalid event edge filter")
	}
	return nil
}

func (j *Journal) EventPage(limit int, cursor string, filter EventFilter) (EventPage, error) {
	if j == nil || j.store == nil {
		return EventPage{}, errors.New("task journal: unavailable")
	}
	page, err := j.store.ListEventsPage(limit, cursor, filter)
	if err != nil {
		j.RecordFailure(err)
		return EventPage{}, err
	}
	for index := range page.Events {
		page.Events[index].Task = j.derive(page.Events[index].Task)
	}
	return page, nil
}

func (s *SQLiteStore) ListEventsPage(limit int, cursor string, filter EventFilter) (EventPage, error) {
	if s == nil || s.db == nil {
		return EventPage{}, errors.New("task journal: store is unavailable")
	}
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize || filter.validate() != nil {
		return EventPage{}, errors.New("task journal: invalid event page")
	}
	after, err := decodeEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}

	query := `SELECT e.event_id,e.task_id,e.task_version,e.sequence,e.occurred_at,e.event_type,e.state,e.operation,
		t.controller,t.safe_summary,t.project_id,t.edge_id,t.created_at
		FROM task_events e JOIN tasks t ON t.task_id=e.task_id WHERE 1=1`
	args := make([]any, 0, 9)
	if after > 0 {
		query += ` AND e.event_id<?`
		args = append(args, after)
	}
	if filter.Controller != "" {
		query += ` AND t.controller=?`
		args = append(args, filter.Controller)
	}
	if filter.State != "" {
		query += ` AND e.state=?`
		args = append(args, filter.State)
	}
	if filter.Operation != "" {
		query += ` AND e.operation=?`
		args = append(args, filter.Operation)
	}
	if filter.EventType != "" {
		query += ` AND e.event_type=?`
		args = append(args, filter.EventType)
	}
	if filter.ProjectID != "" {
		query += ` AND t.project_id=?`
		args = append(args, filter.ProjectID)
	}
	if filter.EdgeID != "" {
		query += ` AND t.edge_id=?`
		args = append(args, filter.EdgeID)
	}
	query += ` ORDER BY e.event_id DESC LIMIT ?`
	args = append(args, limit+1)

	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return EventPage{}, errors.New("task journal: event list failed")
	}
	defer rows.Close()
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		event, err := scanSQLiteEvent(rows)
		if err != nil {
			return EventPage{}, errors.New("task journal: event list result failed")
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, errors.New("task journal: event list iteration failed")
	}
	page := EventPage{Events: events}
	if len(events) > limit {
		page.HasMore = true
		page.Events = events[:limit]
		page.NextCursor = encodeEventCursor(page.Events[len(page.Events)-1].EventID)
	}
	return page, nil
}

func scanSQLiteEvent(row sqliteScanner) (Event, error) {
	var event Event
	var occurred, created int64
	if err := row.Scan(&event.EventID, &event.TaskID, &event.TaskVersion, &event.Sequence, &occurred, &event.EventType, &event.State, &event.Operation,
		&event.Task.Controller, &event.Task.Summary, &event.Task.ProjectID, &event.Task.EdgeID, &created); err != nil {
		return Event{}, err
	}
	event.OccurredAt = time.Unix(0, occurred).UTC()
	event.Task.TaskID = event.TaskID
	event.Task.Sequence = event.Sequence
	event.Task.Operation = event.Operation
	event.Task.State = event.State
	event.Task.CreatedAt = time.Unix(0, created).UTC()
	event.Task.UpdatedAt = event.OccurredAt
	event.Task.HeartbeatAt = event.OccurredAt
	event.Task.Heartbeat = event.OccurredAt
	event.Task.Version = event.TaskVersion
	if isTerminal(event.State) {
		value := event.OccurredAt
		event.Task.TerminalAt = &value
	}
	if err := event.Task.validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func encodeEventCursor(eventID int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(eventID, 10)))
}

func decodeEventCursor(cursor string) (int64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(body) == 0 || len(body) > 24 {
		return 0, errors.New("task journal: invalid event cursor")
	}
	value, err := strconv.ParseInt(string(body), 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("task journal: invalid event cursor")
	}
	return value, nil
}
