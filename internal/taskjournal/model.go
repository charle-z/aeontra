// Package taskjournal persists a bounded, content-free record of externally visible
// MCP operations. It never stores prompts, parameters, results, paths, targets,
// credentials, identities or model reasoning.
package taskjournal

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type State string

const (
	StateRequested        State = "requested"
	StatePlanned          State = "planned"
	StateAwaitingApproval State = "awaiting_approval"
	StateExecuting        State = "executing"
	StateObserving        State = "observing"
	StateValidating       State = "validating"
	StateCompleted        State = "completed"
	StateFailed           State = "failed"
	StateCancelled        State = "cancelled"
	StateDisconnected     State = "disconnected"
)

type StorageState string

const (
	StorageHealthy      StorageState = "healthy"
	StorageNearingLimit StorageState = "nearing_limit"
	StorageDegraded     StorageState = "degraded"
)

const (
	EventStarted    = "started"
	EventHeartbeat  = "heartbeat"
	EventTransition = "transition"
)

var (
	taskIDPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	scopeIDPattern   = regexp.MustCompile(`^(?:prj|edge)_[a-f0-9]{24}$`)
)

var validStates = map[State]struct{}{
	StateRequested: {}, StatePlanned: {}, StateAwaitingApproval: {},
	StateExecuting: {}, StateObserving: {}, StateValidating: {},
	StateCompleted: {}, StateFailed: {}, StateCancelled: {}, StateDisconnected: {},
}

var validControllers = map[string]struct{}{
	"http": {}, "stdio": {}, "internal": {},
}

// Entry is the entire durable and browser-visible operation record. Its JSON
// shape is intentionally closed and contains only server-generated safe fields.
type Entry struct {
	TaskID       string     `json:"task_id"`
	Sequence     int64      `json:"sequence"`
	Controller   string     `json:"controller"`
	Operation    string     `json:"operation"`
	Summary      string     `json:"safe_summary"`
	ProjectID    string     `json:"project_id"`
	EdgeID       string     `json:"edge_id"`
	State        State      `json:"state"`
	DerivedState bool       `json:"derived_state"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	HeartbeatAt  time.Time  `json:"heartbeat_at"`
	Heartbeat    time.Time  `json:"-"`
	TerminalAt   *time.Time `json:"terminal_at"`
	Version      int64      `json:"version"`
}

type Event struct {
	EventID     int64     `json:"event_id"`
	TaskID      string    `json:"task_id"`
	TaskVersion int64     `json:"task_version"`
	Sequence    int64     `json:"sequence"`
	OccurredAt  time.Time `json:"occurred_at"`
	EventType   string    `json:"event_type"`
	State       State     `json:"state"`
	Operation   string    `json:"operation"`
	Task        Entry     `json:"task"`
}

type Status struct {
	Storage      StorageState `json:"storage"`
	Detail       string       `json:"detail"`
	RecordCount  int64        `json:"record_count"`
	DatabaseSize int64        `json:"database_size_bytes"`
	WALSize      int64        `json:"wal_size_bytes"`
}

type Page struct {
	Entries    []Entry
	NextCursor string
	HasMore    bool
}

type TaskFilter struct {
	Controller string
	State      State
	Operation  string
	ProjectID  string
	EdgeID     string
}

func (filter TaskFilter) validate() error {
	if filter.Controller != "" {
		if _, ok := validControllers[filter.Controller]; !ok {
			return errors.New("task journal: invalid controller filter")
		}
	}
	if filter.State != "" {
		if _, ok := validStates[filter.State]; !ok {
			return errors.New("task journal: invalid state filter")
		}
	}
	if filter.Operation != "" && !operationPattern.MatchString(filter.Operation) {
		return errors.New("task journal: invalid operation filter")
	}
	if filter.ProjectID != "" && (!scopeIDPattern.MatchString(filter.ProjectID) || !strings.HasPrefix(filter.ProjectID, "prj_")) {
		return errors.New("task journal: invalid project filter")
	}
	if filter.EdgeID != "" && (!scopeIDPattern.MatchString(filter.EdgeID) || !strings.HasPrefix(filter.EdgeID, "edge_")) {
		return errors.New("task journal: invalid edge filter")
	}
	return nil
}

func newEntry(taskID, operation, controller string, state State, now time.Time) (Entry, error) {
	now = now.UTC()
	entry := Entry{
		TaskID: taskID, Operation: operation, Controller: controller,
		Summary: "MCP tool operation: " + operation, State: state,
		CreatedAt: now, UpdatedAt: now, HeartbeatAt: now, Heartbeat: now, Version: 1,
	}
	if err := entry.validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (e Entry) validate() error {
	if !taskIDPattern.MatchString(e.TaskID) {
		return errors.New("task journal: invalid task id")
	}
	if !operationPattern.MatchString(e.Operation) {
		return errors.New("task journal: invalid operation")
	}
	if e.Summary != "MCP tool operation: "+e.Operation {
		return errors.New("task journal: invalid summary")
	}
	if e.ProjectID != "" && (!scopeIDPattern.MatchString(e.ProjectID) || !strings.HasPrefix(e.ProjectID, "prj_")) {
		return errors.New("task journal: invalid project scope")
	}
	if e.EdgeID != "" && (!scopeIDPattern.MatchString(e.EdgeID) || !strings.HasPrefix(e.EdgeID, "edge_")) {
		return errors.New("task journal: invalid edge scope")
	}
	if _, ok := validStates[e.State]; !ok {
		return errors.New("task journal: invalid state")
	}
	if e.CreatedAt.IsZero() || e.UpdatedAt.IsZero() || e.HeartbeatAt.IsZero() || e.Version < 1 {
		return errors.New("task journal: timestamps and version are required")
	}
	if _, ok := validControllers[e.Controller]; !ok {
		return errors.New("task journal: invalid controller")
	}
	return nil
}

func isTerminal(state State) bool {
	switch state {
	case StateCompleted, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

func canDisconnect(state State) bool {
	switch state {
	case StateExecuting, StateObserving, StateValidating, StateAwaitingApproval:
		return true
	default:
		return false
	}
}
