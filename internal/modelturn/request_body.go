package modelturn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Store) StageRequestBody(ctx context.Context, payload json.RawMessage, canonical bool, ttl time.Duration) (RequestBodyReference, error) {
	var body []byte
	var err error
	if canonical {
		body, err = exactJSON(payload)
	} else {
		body, err = canonicalJSON(payload)
	}
	if err != nil {
		return RequestBodyReference{}, ErrInvalidRequest
	}
	if len(body) == 0 || int64(len(body)) > MaxRequestBodyBytes {
		return RequestBodyReference{}, ErrBodyTooLarge
	}
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl <= 0 || ttl > MaxTurnTTL {
		return RequestBodyReference{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	expiresAt := now.Add(ttl)
	requestRef, err := newOpaqueID("mb_")
	if err != nil {
		return RequestBodyReference{}, errors.New("model body id generation failed")
	}
	digest := digestBytes(body)

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RequestBodyReference{}, errors.New("model body transaction failed")
	}
	defer tx.Rollback()
	if err := s.cleanupLocked(ctx, tx, now); err != nil {
		return RequestBodyReference{}, err
	}
	if err := s.makeRoomLocked(ctx, tx, int64(len(body))); err != nil {
		return RequestBodyReference{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO turn_bodies(body_ref,kind,content,content_bytes,created_at,expires_at) VALUES(?,?,?,?,?,?)`, requestRef, "request", body, len(body), now.UnixNano(), expiresAt.UnixNano()); err != nil {
		return RequestBodyReference{}, errors.New("model request body persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return RequestBodyReference{}, errors.New("model request body commit failed")
	}
	return RequestBodyReference{
		RequestRef:    requestRef,
		RequestDigest: digest,
		ContentBytes:  int64(len(body)),
		ExpiresAt:     expiresAt,
	}, nil
}

func (s *Store) CreateTurnFromReference(ctx context.Context, request ModelRequest) (Turn, error) {
	if !safeIdentifier.MatchString(request.RuntimeID) || request.Sequence == 0 || !strings.HasPrefix(request.RequestRef, "mb_") || !validDigest(request.RequestDigest) {
		return Turn{}, ErrInvalidRequest
	}
	if len(request.Payload) != 0 {
		return Turn{}, ErrInvalidRequest
	}
	offered, err := normalizeOfferedTools(request.OfferedTools)
	if err != nil {
		return Turn{}, err
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl <= 0 || ttl > MaxTurnTTL {
		return Turn{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	requestedExpiresAt := now.Add(ttl)
	turnID, err := newOpaqueID("mt_")
	if err != nil {
		return Turn{}, errors.New("model turn id generation failed")
	}
	offeredJSON, _ := json.Marshal(offered)

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Turn{}, errors.New("model turn transaction failed")
	}
	defer tx.Rollback()
	if err := s.cleanupLocked(ctx, tx, now); err != nil {
		return Turn{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO model_runtimes(runtime_id,status,created_at,updated_at) VALUES(?,'running',?,?)`, request.RuntimeID, now.UnixNano(), now.UnixNano()); err != nil {
		return Turn{}, errors.New("model runtime persistence failed")
	}
	result, err := tx.ExecContext(ctx, `UPDATE model_runtimes SET status='running',updated_at=? WHERE runtime_id=? AND status IN ('ready','running')`, now.UnixNano(), request.RuntimeID)
	if err != nil {
		return Turn{}, errors.New("model runtime activation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Turn{}, ErrTurnConflict
	}
	var maxSequence sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(sequence) FROM model_turns WHERE runtime_id=?`, request.RuntimeID).Scan(&maxSequence); err != nil {
		return Turn{}, errors.New("model turn sequence read failed")
	}
	expected := uint64(1)
	if maxSequence.Valid {
		expected = uint64(maxSequence.Int64) + 1
	}
	if request.Sequence != expected {
		return Turn{}, ErrSequenceMismatch
	}

	var kind string
	var body []byte
	var contentBytes, bodyExpiresNanos int64
	if err := tx.QueryRowContext(ctx, `SELECT kind,content,content_bytes,expires_at FROM turn_bodies WHERE body_ref=?`, request.RequestRef).Scan(&kind, &body, &contentBytes, &bodyExpiresNanos); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Turn{}, ErrRequestRefConflict
		}
		return Turn{}, errors.New("model request body read failed")
	}
	bodyExpiresAt := time.Unix(0, bodyExpiresNanos).UTC()
	if kind != "request" || contentBytes != int64(len(body)) || contentBytes <= 0 || contentBytes > MaxRequestBodyBytes || !now.Before(bodyExpiresAt) || digestBytes(body) != request.RequestDigest {
		return Turn{}, ErrRequestRefConflict
	}
	var alreadyBound int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_turns WHERE request_ref=?`, request.RequestRef).Scan(&alreadyBound); err != nil {
		return Turn{}, errors.New("model request reference binding check failed")
	}
	if alreadyBound != 0 {
		return Turn{}, ErrRequestRefConflict
	}
	expiresAt := requestedExpiresAt
	if bodyExpiresAt.Before(expiresAt) {
		expiresAt = bodyExpiresAt
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO model_turns(turn_id,runtime_id,sequence,request_digest,request_ref,status,created_at,expires_at,offered_tools_json) VALUES(?,?,?,?,?,'created',?,?,?)`, turnID, request.RuntimeID, request.Sequence, request.RequestDigest, request.RequestRef, now.UnixNano(), expiresAt.UnixNano(), string(offeredJSON)); err != nil {
		return Turn{}, ErrRequestRefConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_turns SET status='awaiting_model' WHERE turn_id=? AND status='created'`, turnID); err != nil {
		return Turn{}, errors.New("model turn activation failed")
	}
	if err := tx.Commit(); err != nil {
		return Turn{}, errors.New("model turn commit failed")
	}
	s.signal()
	return Turn{
		RuntimeID:      request.RuntimeID,
		ID:             TurnID(turnID),
		Sequence:       request.Sequence,
		RequestDigest:  request.RequestDigest,
		OfferedToolIDs: offered,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}, nil
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
