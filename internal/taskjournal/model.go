// Package taskjournal persists a bounded, content-free record of externally visible
// MCP operations. It never stores prompts, parameters, results, paths, targets,
// credentials, identities or model reasoning.
package taskjournal

import (
	"errors"
	"regexp"
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

var (
	taskIDPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

var validStates = map[State]struct{}{
	StateRequested: {}, StatePlanned: {}, StateAwaitingApproval: {},
	StateExecuting: {}, StateObserving: {}, StateValidating: {},
	StateCompleted: {}, StateFailed: {}, StateCancelled: {}, StateDisconnected: {},
}

var validControllers = map[string]struct{}{
	"http": {}, "stdio": {}, "internal": {},
}

// Entry is the entire durable and browser-visible task record. Its JSON shape is
// intentionally closed and contains only safe, server-generated fields.
type Entry struct {
	TaskID     string    `json:"task_id"`
	Operation  string    `json:"operation"`
	Summary    string    `json:"summary"`
	State      State     `json:"state"`
	Heartbeat  time.Time `json:"heartbeat"`
	Controller string    `json:"controller"`
}

func newEntry(taskID, operation, controller string, state State, now time.Time) (Entry, error) {
	entry := Entry{
		TaskID: taskID, Operation: operation,
		Summary: "MCP tool operation: " + operation,
		State:   state, Heartbeat: now.UTC(), Controller: controller,
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
	if _, ok := validStates[e.State]; !ok {
		return errors.New("task journal: invalid state")
	}
	if e.Heartbeat.IsZero() {
		return errors.New("task journal: heartbeat is required")
	}
	if _, ok := validControllers[e.Controller]; !ok {
		return errors.New("task journal: invalid controller")
	}
	return nil
}

func isTerminal(state State) bool {
	switch state {
	case StateCompleted, StateFailed, StateCancelled, StateDisconnected:
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
