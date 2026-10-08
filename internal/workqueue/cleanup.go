package workqueue

import (
	"errors"
	"time"
)

func (s *Store) CleanupTerminal(retention time.Duration, limit int) (int64, error) {
	if s == nil || s.db == nil || retention < time.Minute || retention > 365*24*time.Hour || limit < 1 || limit > MaxListResults {
		return 0, errors.New("workqueue: cleanup request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.clock().UTC().Add(-retention).UnixNano()
	result, err := s.db.Exec(`DELETE FROM jobs WHERE job_id IN (
		SELECT j.job_id FROM jobs j
		WHERE j.state IN (?,?,?) AND j.updated_at<?
		AND NOT EXISTS (SELECT 1 FROM dependencies d WHERE d.dependency_id=j.job_id)
		AND NOT EXISTS (SELECT 1 FROM task_workers tw WHERE tw.job_id=j.job_id)
		ORDER BY j.updated_at,j.job_id LIMIT ?
	)`, StateSucceeded, StateFailed, StateCancelled, cutoff, limit)
	if err != nil {
		return 0, errors.New("workqueue: cleanup failed")
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, errors.New("workqueue: cleanup result unavailable")
	}
	return removed, nil
}
