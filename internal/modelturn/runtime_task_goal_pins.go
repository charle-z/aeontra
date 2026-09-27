package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	TaskGoalPinOrphanGrace = 5 * time.Minute
	maxTaskGoalPinOwners   = 4096
	maxTaskGoalPinRows     = 20_480
	maxTaskGoalRefsPerTask = 4
)

type taskGoalPinRow struct {
	ownerDigest string
	bodyRef     string
	createdAt   int64
}

// PinTaskGoalReferences durably protects staged goals before their task group is
// committed to the separate workqueue database.
func (s *Store) PinTaskGoalReferences(ctx context.Context, ownerDigest string, refs []TaskGoalReference) error {
	if s == nil || s.db == nil || !idempotencyDigestPattern.MatchString(ownerDigest) || !validTaskGoalReferences(refs) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("task goal pin transaction failed")
	}
	defer tx.Rollback()
	if err := insertVerifiedTaskGoalPins(ctx, tx, ownerDigest, refs, s.now().UTC()); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_goal_pins`).Scan(&count); err != nil || count > maxTaskGoalPinRows {
		return errors.New("task goal pin bound exceeded")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("task goal pin commit failed")
	}
	return nil
}

// UnpinTaskGoalReferences removes only the named owner's exact references. Expired
// bodies remain subject to the normal bounded cleanup path.
func (s *Store) UnpinTaskGoalReferences(ctx context.Context, ownerDigest string, refs []TaskGoalReference) error {
	if s == nil || s.db == nil || !idempotencyDigestPattern.MatchString(ownerDigest) || !validTaskGoalReferences(refs) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("task goal unpin transaction failed")
	}
	defer tx.Rollback()
	for _, ref := range refs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_goal_pins WHERE owner_digest=? AND body_ref=? AND content_digest=?`, ownerDigest, ref.BodyRef, ref.ContentDigest); err != nil {
			return errors.New("task goal unpin failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("task goal unpin commit failed")
	}
	return nil
}

// ReconcileTaskGoalPins restores active task ownership and reclaims old orphaned
// pins. Newly written pins are retained for orphanGrace so a concurrent staged
// task cannot be unpinned between its pin and queue commits.
func (s *Store) ReconcileTaskGoalPins(ctx context.Context, owners []TaskGoalOwner, orphanGrace time.Duration) error {
	if s == nil || s.db == nil || orphanGrace < 0 || orphanGrace > TaskGoalPinOrphanGrace || !validTaskGoalOwners(owners) {
		return ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("task goal pin reconciliation failed")
	}
	defer tx.Rollback()
	now := s.now().UTC()
	expected := make(map[string]struct{})
	for _, owner := range owners {
		if err := insertVerifiedTaskGoalPins(ctx, tx, owner.OwnerDigest, owner.References, now); err != nil {
			return err
		}
		for _, ref := range owner.References {
			expected[owner.OwnerDigest+"\x00"+ref.BodyRef] = struct{}{}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT owner_digest,body_ref,created_at FROM runtime_goal_pins ORDER BY owner_digest,body_ref LIMIT ?`, maxTaskGoalPinRows+1)
	if err != nil {
		return errors.New("task goal pin reconciliation failed")
	}
	pins := make([]taskGoalPinRow, 0)
	for rows.Next() {
		var pin taskGoalPinRow
		if err := rows.Scan(&pin.ownerDigest, &pin.bodyRef, &pin.createdAt); err != nil {
			_ = rows.Close()
			return errors.New("task goal pin reconciliation failed")
		}
		pins = append(pins, pin)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return errors.New("task goal pin reconciliation failed")
	}
	if err := rows.Close(); err != nil || len(pins) > maxTaskGoalPinRows {
		return errors.New("task goal pin bound exceeded")
	}
	cutoff := now.Add(-orphanGrace).UnixNano()
	for _, pin := range pins {
		if _, found := expected[pin.ownerDigest+"\x00"+pin.bodyRef]; found || pin.createdAt > cutoff {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_goal_pins WHERE owner_digest=? AND body_ref=? AND created_at<=?`, pin.ownerDigest, pin.bodyRef, cutoff); err != nil {
			return errors.New("task goal orphan cleanup failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("task goal pin reconciliation commit failed")
	}
	return nil
}

func insertVerifiedTaskGoalPins(ctx context.Context, tx *sql.Tx, ownerDigest string, refs []TaskGoalReference, now time.Time) error {
	for _, ref := range refs {
		var digest string
		if err := tx.QueryRowContext(ctx, `SELECT content_digest FROM runtime_bodies WHERE body_ref=? AND kind='goal'`, ref.BodyRef).Scan(&digest); err != nil || digest != ref.ContentDigest {
			return ErrRequestRefConflict
		}
		var existingDigest string
		err := tx.QueryRowContext(ctx, `SELECT content_digest FROM runtime_goal_pins WHERE owner_digest=? AND body_ref=?`, ownerDigest, ref.BodyRef).Scan(&existingDigest)
		if err == nil && existingDigest != ref.ContentDigest {
			return ErrRequestRefConflict
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return errors.New("task goal pin lookup failed")
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO runtime_goal_pins(owner_digest,body_ref,content_digest,created_at) VALUES(?,?,?,?)`, ownerDigest, ref.BodyRef, ref.ContentDigest, now.UnixNano()); err != nil {
			return errors.New("task goal pin write failed")
		}
	}
	return nil
}

func validTaskGoalReferences(refs []TaskGoalReference) bool {
	if len(refs) < 1 || len(refs) > maxTaskGoalRefsPerTask {
		return false
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if !resultReferencePattern.MatchString(ref.BodyRef) || !strings.HasPrefix(ref.BodyRef, "mb_") || !goalDigestPattern.MatchString(ref.ContentDigest) {
			return false
		}
		if _, found := seen[ref.BodyRef]; found {
			return false
		}
		seen[ref.BodyRef] = struct{}{}
	}
	return true
}

func validTaskGoalOwners(owners []TaskGoalOwner) bool {
	if len(owners) > maxTaskGoalPinOwners {
		return false
	}
	seen := make(map[string]struct{}, len(owners))
	for _, owner := range owners {
		if !idempotencyDigestPattern.MatchString(owner.OwnerDigest) || !validTaskGoalReferences(owner.References) {
			return false
		}
		if _, found := seen[owner.OwnerDigest]; found {
			return false
		}
		seen[owner.OwnerDigest] = struct{}{}
	}
	return true
}
