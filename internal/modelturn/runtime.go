package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RuntimeStatus string

const (
	RuntimeReady     RuntimeStatus = "ready"
	RuntimeRunning   RuntimeStatus = "running"
	RuntimeCompleted RuntimeStatus = "completed"
	RuntimeCancelled RuntimeStatus = "cancelled"
	RuntimeFailed    RuntimeStatus = "failed"
)

type Runtime struct {
	RuntimeID        string        `json:"runtime_id"`
	Status           RuntimeStatus `json:"status"`
	LastSequence     uint64        `json:"last_sequence"`
	ActiveTurnID     TurnID        `json:"active_turn_id"`
	ActiveTurnStatus Status        `json:"active_turn_status"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

func (s *Store) StartRuntime(ctx context.Context) (Runtime, error) {
	runtimeID, err := newOpaqueID("mr_")
	if err != nil {
		return Runtime{}, errors.New("model runtime id generation failed")
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO model_runtimes(runtime_id,status,created_at,updated_at) VALUES(?,'ready',?,?)`, runtimeID, now.UnixNano(), now.UnixNano()); err != nil {
		return Runtime{}, errors.New("model runtime persistence failed")
	}
	return Runtime{RuntimeID: runtimeID, Status: RuntimeReady, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) SetRuntimeRunning(ctx context.Context, runtimeID string) error {
	return s.transitionRuntime(ctx, runtimeID, RuntimeRunning, RuntimeReady, RuntimeRunning)
}

func (s *Store) CompleteRuntime(ctx context.Context, runtimeID string) error {
	return s.transitionRuntime(ctx, runtimeID, RuntimeCompleted, RuntimeReady, RuntimeRunning)
}

func (s *Store) FailRuntime(ctx context.Context, runtimeID string) error {
	return s.transitionRuntime(ctx, runtimeID, RuntimeFailed, RuntimeReady, RuntimeRunning)
}

func (s *Store) transitionRuntime(ctx context.Context, runtimeID string, target RuntimeStatus, allowed ...RuntimeStatus) error {
	if !safeIdentifier.MatchString(runtimeID) {
		return ErrInvalidRequest
	}
	now := s.now().UTC()
	args := make([]any, 0, len(allowed)+3)
	query := `UPDATE model_runtimes SET status=?,updated_at=? WHERE runtime_id=? AND status IN (`
	args = append(args, target, now.UnixNano(), runtimeID)
	for index, status := range allowed {
		if index > 0 {
			query += ","
		}
		query += "?"
		args = append(args, status)
	}
	query += ")"
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return errors.New("model runtime transition failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	s.signal()
	return nil
}

func (s *Store) CancelRuntime(ctx context.Context, runtimeID string) error {
	if !safeIdentifier.MatchString(runtimeID) {
		return ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("model runtime cancellation failed")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET status='cancelled',updated_at=? WHERE runtime_id=? AND status IN ('ready','running')`, now.UnixNano(), runtimeID)
	if err != nil {
		return errors.New("model runtime cancellation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ErrTurnConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='cancelled' WHERE runtime_id=? AND status IN ('created','awaiting_model','disconnected')`, runtimeID); err != nil {
		return errors.New("model runtime turn cancellation failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("model runtime cancellation commit failed")
	}
	s.signal()
	return nil
}

func (s *Store) Runtime(ctx context.Context, runtimeID string) (Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Runtime{}, ErrInvalidRequest
	}
	var runtime Runtime
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `SELECT runtime_id,status,created_at,updated_at FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(&runtime.RuntimeID, &runtime.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, ErrTurnNotFound
	}
	if err != nil {
		return Runtime{}, errors.New("model runtime read failed")
	}
	runtime.CreatedAt = time.Unix(0, createdAt).UTC()
	runtime.UpdatedAt = time.Unix(0, updatedAt).UTC()
	var turnID sql.NullString
	var status sql.NullString
	var sequence sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT turn_id,status,sequence FROM model_turns WHERE runtime_id=? ORDER BY sequence DESC LIMIT 1`, runtimeID).Scan(&turnID, &status, &sequence)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, errors.New("model runtime turn read failed")
	}
	if turnID.Valid {
		runtime.ActiveTurnID = TurnID(turnID.String)
		runtime.ActiveTurnStatus = Status(status.String)
		runtime.LastSequence = uint64(sequence.Int64)
	}
	return runtime, nil
}

func (s *Store) Poll(ctx context.Context, runtimeID string) (Offer, bool, error) {
	offer, err := s.nextOnce(ctx, runtimeID)
	if errors.Is(err, ErrTurnNotFound) {
		return Offer{}, false, nil
	}
	return offer, err == nil, err
}

func (s *Store) PollAfter(ctx context.Context, runtimeID string, afterSequence uint64) (Offer, bool, Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Offer{}, false, Runtime{}, ErrInvalidRequest
	}
	offer, err := s.nextOnce(ctx, runtimeID)
	if err == nil && offer.Sequence > afterSequence {
		runtime, runtimeErr := s.Runtime(ctx, runtimeID)
		return offer, true, runtime, runtimeErr
	}
	if err != nil && !errors.Is(err, ErrTurnNotFound) {
		return Offer{}, false, Runtime{}, err
	}
	runtime, err := s.Runtime(ctx, runtimeID)
	if err != nil {
		return Offer{}, false, Runtime{}, err
	}
	return Offer{}, false, runtime, nil
}

func (s *Store) WaitNextAfter(ctx context.Context, runtimeID string, afterSequence uint64) (Offer, bool, Runtime, error) {
	if !safeIdentifier.MatchString(runtimeID) {
		return Offer{}, false, Runtime{}, ErrInvalidRequest
	}
	var lastRuntime Runtime
	for {
		if err := ctx.Err(); err != nil {
			return Offer{}, false, lastRuntime, err
		}
		wake := s.waitChannel()
		offer, pending, runtime, err := s.PollAfter(ctx, runtimeID, afterSequence)
		if runtime.RuntimeID != "" {
			lastRuntime = runtime
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Offer{}, false, runtime, ctxErr
			}
			return Offer{}, false, runtime, err
		}
		if pending || runtimeStopsWait(runtime, afterSequence) {
			return offer, pending, runtime, nil
		}
		select {
		case <-ctx.Done():
			return Offer{}, false, runtime, ctx.Err()
		case <-wake:
		case <-time.After(time.Second):
		}
	}
}

func runtimeStopsWait(runtime Runtime, afterSequence uint64) bool {
	switch runtime.Status {
	case RuntimeCompleted, RuntimeCancelled, RuntimeFailed:
		return true
	}
	if runtime.LastSequence > afterSequence {
		return false
	}
	switch runtime.ActiveTurnStatus {
	case StatusDisconnected, StatusCancelled, StatusExpired, StatusFailed:
		return true
	default:
		return false
	}
}
