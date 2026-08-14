package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const MaxRuntimePhaseEvents = 32

type RuntimePhase string
type RuntimeRetryCategory string

const (
	RuntimePhaseCreated                RuntimePhase = "runtime_created"
	RuntimePhaseLeaseAssigned          RuntimePhase = "lease_assigned"
	RuntimePhaseLeaseRetry             RuntimePhase = "lease_retry"
	RuntimePhaseLocalPreflightComplete RuntimePhase = "local_preflight_completed"
	RuntimePhaseStartedConfirmed       RuntimePhase = "started_confirmed"
	RuntimePhaseDriverSocketReady      RuntimePhase = "driver_socket_ready"
	RuntimePhaseOpenCodeProcessStarted RuntimePhase = "opencode_process_started"
	RuntimePhaseModelAdapterReady      RuntimePhase = "model_adapter_ready"
	RuntimePhaseCodexProcessStarted    RuntimePhase = "codex_process_started"
	RuntimePhaseFirstTurnCreated       RuntimePhase = "first_model_turn_created"
	RuntimePhaseToolExecutionStarted   RuntimePhase = "tool_execution_started"
	RuntimePhaseTerminal               RuntimePhase = "terminal"
)

const (
	RuntimeRetryNoContent           RuntimeRetryCategory = "no_content"
	RuntimeRetryClientTimeout       RuntimeRetryCategory = "client_timeout"
	RuntimeRetryTransportError      RuntimeRetryCategory = "transport_error"
	RuntimeRetryServerBusy          RuntimeRetryCategory = "server_busy"
	RuntimeRetryUpstreamUnavailable RuntimeRetryCategory = "upstream_unavailable"
	RuntimeRetryGatewayTimeout      RuntimeRetryCategory = "gateway_timeout"
)

type RuntimePhaseEvent struct {
	Phase          RuntimePhase         `json:"phase"`
	Timestamp      time.Time            `json:"timestamp"`
	LastTimestamp  time.Time            `json:"last_timestamp,omitempty"`
	DurationMS     int64                `json:"duration_ms"`
	SinceCreatedMS int64                `json:"since_created_ms"`
	RetryCategory  RuntimeRetryCategory `json:"retry_category,omitempty"`
	Count          uint32               `json:"count,omitempty"`
}

type runtimePhaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) RecordRuntimePhase(ctx context.Context, runtimeID string, phase RuntimePhase, category RuntimeRetryCategory, count uint32) error {
	if !safeIdentifier.MatchString(runtimeID) || !validRuntimePhase(phase) || !validRuntimePhaseCategory(phase, category, count) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recordRuntimePhaseLocked(ctx, s.db, runtimeID, phase, category, count, s.now().UTC()); err != nil {
		return err
	}
	s.signal()
	return nil
}

func (s *Store) recordRuntimePhaseLocked(ctx context.Context, executor runtimePhaseExecutor, runtimeID string, phase RuntimePhase, category RuntimeRetryCategory, count uint32, now time.Time) error {
	var exists int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(&exists); err != nil || exists != 1 {
		if err == nil {
			return ErrTurnNotFound
		}
		return errors.New("model runtime phase lookup failed")
	}
	var lastAt sql.NullInt64
	if err := executor.QueryRowContext(ctx, `SELECT MAX(last_at) FROM runtime_phase_events WHERE runtime_id=?`, runtimeID).Scan(&lastAt); err != nil {
		return errors.New("model runtime phase clock read failed")
	}
	stamp := now.UnixNano()
	if lastAt.Valid && stamp <= lastAt.Int64 {
		stamp = lastAt.Int64 + 1
	}
	if phase == RuntimePhaseLeaseRetry {
		_, err := executor.ExecContext(ctx, `INSERT INTO runtime_phase_events(runtime_id,phase,category,count,occurred_at,last_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(runtime_id,phase,category) DO UPDATE SET count=MIN(runtime_phase_events.count+excluded.count,1000),last_at=MAX(runtime_phase_events.last_at,excluded.last_at)`, runtimeID, phase, category, count, stamp, stamp)
		if err != nil {
			return errors.New("model runtime retry phase persistence failed")
		}
		return nil
	}
	result, err := executor.ExecContext(ctx, `INSERT OR IGNORE INTO runtime_phase_events(runtime_id,phase,category,count,occurred_at,last_at) VALUES(?,?,'',1,?,?)`, runtimeID, phase, stamp, stamp)
	if err != nil {
		return errors.New("model runtime phase persistence failed")
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil
	}
	var total int
	if err := executor.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_phase_events WHERE runtime_id=?`, runtimeID).Scan(&total); err != nil {
		return errors.New("model runtime phase limit check failed")
	}
	if total > MaxRuntimePhaseEvents {
		return errors.New("model runtime phase limit exceeded")
	}
	return nil
}

func (s *Store) runtimePhasesLocked(ctx context.Context, runtime Runtime) ([]RuntimePhaseEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT phase,category,count,occurred_at,last_at FROM runtime_phase_events WHERE runtime_id=? ORDER BY occurred_at,phase,category`, runtime.RuntimeID)
	if err != nil {
		return nil, errors.New("model runtime phase read failed")
	}
	defer rows.Close()
	phases := make([]RuntimePhaseEvent, 0, 12)
	previous := runtime.CreatedAt
	for rows.Next() {
		var phase RuntimePhase
		var category RuntimeRetryCategory
		var count uint32
		var occurredAt, lastAt int64
		if err := rows.Scan(&phase, &category, &count, &occurredAt, &lastAt); err != nil {
			return nil, errors.New("model runtime phase read failed")
		}
		stamp := time.Unix(0, occurredAt).UTC()
		last := time.Unix(0, lastAt).UTC()
		if stamp.Before(runtime.CreatedAt) {
			stamp = runtime.CreatedAt
		}
		if stamp.Before(previous) {
			stamp = previous
		}
		if last.Before(stamp) {
			last = stamp
		}
		phases = append(phases, RuntimePhaseEvent{
			Phase: phase, Timestamp: stamp, LastTimestamp: last,
			DurationMS:     nonNegativeMilliseconds(stamp.Sub(previous)),
			SinceCreatedMS: nonNegativeMilliseconds(stamp.Sub(runtime.CreatedAt)),
			RetryCategory:  category, Count: count,
		})
		previous = last
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("model runtime phase read failed")
	}
	return phases, nil
}

func nonNegativeMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return value.Milliseconds()
}

func validRuntimePhase(phase RuntimePhase) bool {
	switch phase {
	case RuntimePhaseCreated, RuntimePhaseLeaseAssigned, RuntimePhaseLeaseRetry, RuntimePhaseLocalPreflightComplete, RuntimePhaseStartedConfirmed, RuntimePhaseDriverSocketReady, RuntimePhaseOpenCodeProcessStarted, RuntimePhaseModelAdapterReady, RuntimePhaseCodexProcessStarted, RuntimePhaseFirstTurnCreated, RuntimePhaseToolExecutionStarted, RuntimePhaseTerminal:
		return true
	default:
		return false
	}
}

func validRuntimePhaseCategory(phase RuntimePhase, category RuntimeRetryCategory, count uint32) bool {
	if phase != RuntimePhaseLeaseRetry {
		return category == "" && count == 1
	}
	if count == 0 || count > 100 {
		return false
	}
	switch category {
	case RuntimeRetryNoContent, RuntimeRetryClientTimeout, RuntimeRetryTransportError, RuntimeRetryServerBusy, RuntimeRetryUpstreamUnavailable, RuntimeRetryGatewayTimeout:
		return true
	default:
		return false
	}
}

func EdgeReportableRuntimePhase(phase RuntimePhase) bool {
	return phase == RuntimePhaseLocalPreflightComplete || phase == RuntimePhaseDriverSocketReady || phase == RuntimePhaseOpenCodeProcessStarted || phase == RuntimePhaseModelAdapterReady || phase == RuntimePhaseCodexProcessStarted || phase == RuntimePhaseLeaseRetry
}

func RuntimeAcceptsEdgePhase(runtime Runtime, phase RuntimePhase) bool {
	if !EdgeReportableRuntimePhase(phase) {
		return false
	}
	switch runtime.State {
	case RuntimeStateCompleted, RuntimeStateFailed, RuntimeStateCancelled, RuntimeStateExpired:
		return false
	}
	hasPhase := func(target RuntimePhase) bool {
		for _, event := range runtime.Phases {
			if event.Phase == target {
				return true
			}
		}
		return false
	}
	if !hasPhase(RuntimePhaseLeaseAssigned) {
		return false
	}
	switch phase {
	case RuntimePhaseLeaseRetry:
		return runtime.State == RuntimeStateStarting && !hasPhase(RuntimePhaseLocalPreflightComplete) && !hasPhase(RuntimePhaseStartedConfirmed)
	case RuntimePhaseLocalPreflightComplete:
		return runtime.State == RuntimeStateStarting && !hasPhase(RuntimePhaseStartedConfirmed)
	case RuntimePhaseDriverSocketReady:
		return runtime.State == RuntimeStateAwaitingModel && hasPhase(RuntimePhaseStartedConfirmed)
	case RuntimePhaseOpenCodeProcessStarted:
		return runtime.State == RuntimeStateAwaitingModel && hasPhase(RuntimePhaseDriverSocketReady)
	case RuntimePhaseModelAdapterReady:
		return runtime.State == RuntimeStateAwaitingModel && hasPhase(RuntimePhaseStartedConfirmed)
	case RuntimePhaseCodexProcessStarted:
		return runtime.State == RuntimeStateAwaitingModel && hasPhase(RuntimePhaseModelAdapterReady)
	default:
		return false
	}
}
