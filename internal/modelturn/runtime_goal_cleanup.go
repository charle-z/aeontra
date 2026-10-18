package modelturn

import (
	"context"
	"errors"
)

// DiscardRuntimeGoal removes one staged goal only when no authoritative runtime
// references it. It is used to clean up idempotent duplicate starts without
// allowing callers to mutate the goal of an existing runtime.
func (s *Store) DiscardRuntimeGoal(ctx context.Context, bodyRef, contentDigest string) error {
	if s == nil || s.db == nil || !resultReferencePattern.MatchString(bodyRef) || !goalDigestPattern.MatchString(contentDigest) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM runtime_bodies
		WHERE body_ref=? AND kind='goal' AND content_digest=?
		AND NOT EXISTS (SELECT 1 FROM model_runtimes WHERE goal_ref=?)`, bodyRef, contentDigest, bodyRef); err != nil {
		return errors.New("runtime goal cleanup failed")
	}
	return nil
}
