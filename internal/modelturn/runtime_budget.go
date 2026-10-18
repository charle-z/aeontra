package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) runtimeTurnDeadlineLocked(ctx context.Context, tx *sql.Tx, runtimeID string, sequence uint64, requestedTTL time.Duration, now time.Time) (time.Time, error) {
	var controller RuntimeController
	var expiresAtNanos int64
	var executionTimeoutSeconds int
	if err := tx.QueryRowContext(ctx, `SELECT controller,expires_at,execution_timeout_seconds FROM model_runtimes WHERE runtime_id=?`, runtimeID).Scan(&controller, &expiresAtNanos, &executionTimeoutSeconds); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, ErrTurnNotFound
		}
		return time.Time{}, errors.New("model runtime execution budget read failed")
	}
	requestedDeadline := now.Add(requestedTTL)
	if controller != ControllerRemoteEdge || executionTimeoutSeconds <= 0 {
		return requestedDeadline, nil
	}
	executionTTL := time.Duration(executionTimeoutSeconds) * time.Second
	if executionTTL <= 0 || executionTTL > MaxTurnTTL {
		return time.Time{}, ErrInvalidRequest
	}
	executionDeadline := time.Unix(0, expiresAtNanos).UTC()
	if sequence == 1 {
		executionDeadline = now.Add(executionTTL)
		result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET expires_at=?,updated_at=? WHERE runtime_id=? AND controller='remote_edge' AND last_sequence=0 AND state NOT IN ('completed','failed','cancelled','expired')`, executionDeadline.UnixNano(), now.UnixNano(), runtimeID)
		if err != nil {
			return time.Time{}, errors.New("model runtime execution budget activation failed")
		}
		rows, _ := result.RowsAffected()
		if rows != 1 {
			return time.Time{}, ErrTurnConflict
		}
	}
	if !now.Before(executionDeadline) {
		return time.Time{}, ErrLateResponse
	}
	if executionDeadline.Before(requestedDeadline) {
		return executionDeadline, nil
	}
	return requestedDeadline, nil
}
