package modelturn

import (
	"context"
	"database/sql"
	"errors"
)

// PinnedDevelopmentBody returns goal content only through the exact durable
// owner/reference/digest binding. Expiry does not revoke an existing pin.
func (s *Store) PinnedDevelopmentBody(ctx context.Context, ownerDigest string, ref TaskGoalReference) ([]byte, error) {
	if s == nil || s.db == nil || ctx == nil || !idempotencyDigestPattern.MatchString(ownerDigest) ||
		!validTaskGoalReferences([]TaskGoalReference{ref}) {
		return nil, ErrRequestRefConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var content []byte
	var digest string
	var contentBytes int64
	err := s.db.QueryRowContext(ctx, `SELECT b.content,b.content_digest,b.content_bytes
		FROM runtime_goal_pins p JOIN runtime_bodies b
		ON b.body_ref=p.body_ref AND b.content_digest=p.content_digest
		WHERE p.owner_digest=? AND p.body_ref=? AND p.content_digest=? AND b.kind='goal'`,
		ownerDigest, ref.BodyRef, ref.ContentDigest).Scan(&content, &digest, &contentBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRequestRefConflict
	}
	if err != nil {
		return nil, errors.New("pinned development body lookup failed")
	}
	if contentBytes <= 0 || contentBytes > MaxGoalBodyBytes || contentBytes != int64(len(content)) || digest != ref.ContentDigest || digestBytes(content) != ref.ContentDigest {
		return nil, ErrRequestRefConflict
	}
	return append([]byte(nil), content...), nil
}
