package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) expireRuntimesLocked(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT runtime_id FROM model_runtimes WHERE expires_at>0 AND expires_at<=? AND state NOT IN ('completed','failed','cancelled','expired') ORDER BY runtime_id`, now.UnixNano())
	if err != nil {
		return errors.New("model runtime expiry phase lookup failed")
	}
	expiring := make([]string, 0)
	for rows.Next() {
		var runtimeID string
		if err := rows.Scan(&runtimeID); err != nil {
			_ = rows.Close()
			return errors.New("model runtime expiry phase lookup failed")
		}
		expiring = append(expiring, runtimeID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return errors.New("model runtime expiry phase lookup failed")
	}
	if err := rows.Close(); err != nil {
		return errors.New("model runtime expiry phase lookup failed")
	}
	for _, runtimeID := range expiring {
		if err := s.recordRuntimePhaseLocked(ctx, tx, runtimeID, RuntimePhaseTerminal, "", 1, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET state='expired',status='failed',updated_at=? WHERE expires_at>0 AND expires_at<=? AND state NOT IN ('completed','failed','cancelled','expired')`, now.UnixNano(), now.UnixNano()); err != nil {
		return errors.New("model runtime expiry failed")
	}
	return nil
}
