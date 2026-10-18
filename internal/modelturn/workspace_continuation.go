package modelturn

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func validWorkspaceContinuationRequest(request BoundRuntimeRequest) bool {
	return deviceIDPattern.MatchString(request.DeviceID) &&
		workspaceIDPattern.MatchString(request.WorkspaceID) &&
		request.Controller == ControllerRemoteEdge &&
		goalSummaryPattern.MatchString(request.GoalSummary) &&
		strings.HasPrefix(request.GoalRef, "mb_") &&
		goalDigestPattern.MatchString(request.GoalDigest) &&
		idempotencyDigestPattern.MatchString(request.IdempotencyKeyDigest) &&
		request.TTL > 0 && request.TTL <= MaxTurnTTL
}

func (s *Store) StartWorkspaceContinuationRuntime(ctx context.Context, request BoundRuntimeRequest) (Runtime, bool, error) {
	if !validWorkspaceContinuationRequest(request) {
		return Runtime{}, false, ErrInvalidRequest
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	var goalDigest string
	if err := s.db.QueryRowContext(ctx, `SELECT content_digest FROM runtime_bodies WHERE body_ref=? AND kind='goal' AND expires_at>?`, request.GoalRef, now.UnixNano()).Scan(&goalDigest); err != nil {
		return Runtime{}, false, ErrRequestRefConflict
	}
	if goalDigest != request.GoalDigest || request.GoalSummary != "goal:sha256:"+goalDigest[len("sha256:"):len("sha256:")+24] {
		return Runtime{}, false, ErrRequestRefConflict
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE model_runtimes SET state='expired',status='failed',updated_at=? WHERE state NOT IN ('completed','failed','cancelled','expired') AND expires_at<=?`, now.UnixNano(), now.UnixNano()); err != nil {
		return Runtime{}, false, errors.New("model runtime expiry failed")
	}
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT runtime_id FROM model_runtimes WHERE device_id=? AND idempotency_key_digest=?`, request.DeviceID, request.IdempotencyKeyDigest).Scan(&existingID)
	if err == nil {
		runtime, readErr := s.runtimeLocked(ctx, existingID)
		if readErr != nil {
			return Runtime{}, false, readErr
		}
		if runtime.WorkspaceID != request.WorkspaceID || runtime.Controller != request.Controller || runtime.GoalSummary != request.GoalSummary || runtime.goalDigest != request.GoalDigest {
			return Runtime{}, false, ErrTurnConflict
		}
		return runtime, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, false, errors.New("model runtime idempotency lookup failed")
	}
	err = s.db.QueryRowContext(ctx, `SELECT runtime_id FROM model_runtimes WHERE workspace_id=? AND state NOT IN ('completed','failed','cancelled','expired') AND expires_at>? ORDER BY created_at,runtime_id LIMIT 1`, request.WorkspaceID, now.UnixNano()).Scan(&existingID)
	if err == nil {
		runtime, readErr := s.runtimeLocked(ctx, existingID)
		if readErr != nil {
			return Runtime{}, false, readErr
		}
		if runtime.DeviceID != request.DeviceID || runtime.Controller != request.Controller || runtime.GoalSummary != request.GoalSummary || runtime.goalDigest != request.GoalDigest {
			return Runtime{}, false, ErrTurnConflict
		}
		return runtime, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Runtime{}, false, errors.New("active model runtime lookup failed")
	}
	runtime, err := s.startRuntimeLocked(ctx, request, now)
	return runtime, err == nil, err
}
