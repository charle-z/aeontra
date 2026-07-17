package modelturn

import (
	"context"
	"errors"
	"time"
)

const MaxConsoleRuntimes = 200

type ConsoleRuntime struct {
	RuntimeID    string
	State        RuntimeState
	Controller   RuntimeController
	LastActivity time.Time
	Active       bool
}

func (s *Store) ConsoleRuntimes(ctx context.Context, limit int) ([]ConsoleRuntime, error) {
	if s == nil || s.db == nil {
		return []ConsoleRuntime{}, nil
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > MaxConsoleRuntimes {
		return nil, errors.New("model runtime console limit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT runtime_id,state,controller,
		CASE WHEN last_heartbeat>updated_at THEN last_heartbeat ELSE updated_at END
		FROM model_runtimes ORDER BY updated_at DESC,runtime_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, errors.New("model runtime console snapshot failed")
	}
	defer rows.Close()
	runtimes := make([]ConsoleRuntime, 0, limit)
	for rows.Next() {
		var runtime ConsoleRuntime
		var activity int64
		if err := rows.Scan(&runtime.RuntimeID, &runtime.State, &runtime.Controller, &activity); err != nil {
			return nil, errors.New("model runtime console snapshot invalid")
		}
		if !safeIdentifier.MatchString(runtime.RuntimeID) || !validRuntimeState(runtime.State) || !validRuntimeController(runtime.Controller) {
			continue
		}
		runtime.LastActivity = time.Unix(0, activity).UTC()
		runtime.Active = !terminalRuntimeState(runtime.State)
		runtimes = append(runtimes, runtime)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("model runtime console snapshot failed")
	}
	return runtimes, nil
}

func validRuntimeController(controller RuntimeController) bool {
	switch controller {
	case ControllerPullRendezvous, ControllerRemoteEdge, ControllerMCPSampling:
		return true
	default:
		return false
	}
}

func terminalRuntimeState(state RuntimeState) bool {
	switch state {
	case RuntimeStateCompleted, RuntimeStateFailed, RuntimeStateCancelled, RuntimeStateExpired:
		return true
	default:
		return false
	}
}
