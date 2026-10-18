package modelturn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// TurnBySequence returns the authoritative identity for an already-created turn.
// It is used to make remote create retries idempotent without creating a second
// request body or consuming a second sequence number.
func (s *Store) TurnBySequence(ctx context.Context, runtimeID string, sequence uint64) (Turn, error) {
	if !safeIdentifier.MatchString(runtimeID) || sequence == 0 {
		return Turn{}, ErrInvalidRequest
	}
	row := s.db.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json FROM model_turns WHERE runtime_id=? AND sequence=?`, runtimeID, sequence)
	record, offeredJSON, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Turn{}, ErrTurnNotFound
	}
	if err != nil {
		return Turn{}, errors.New("model turn read failed")
	}
	var offered []string
	if err := json.Unmarshal([]byte(offeredJSON), &offered); err != nil {
		return Turn{}, errors.New("offered tool metadata is invalid")
	}
	return Turn{
		RuntimeID:      record.RuntimeID,
		ID:             record.TurnID,
		Sequence:       record.Sequence,
		RequestDigest:  record.RequestDigest,
		RequestRef:     record.RequestRef,
		OfferedToolIDs: offered,
		CreatedAt:      record.CreatedAt,
		ExpiresAt:      record.ExpiresAt,
	}, nil
}

// ResponseSnapshot reads a persisted response without changing its consumption
// state. A signed Edge wait receipt uses this only to replay the same response
// after the first delivery was consumed but the HTTP reply was lost.
func (s *Store) ResponseSnapshot(ctx context.Context, turnID TurnID) (ModelResponse, Status, error) {
	if !safeIdentifier.MatchString(string(turnID)) {
		return ModelResponse{}, "", ErrInvalidRequest
	}
	row := s.db.QueryRowContext(ctx, `SELECT turn_id,runtime_id,sequence,request_digest,request_ref,response_digest,response_ref,status,created_at,expires_at,responded_at,consumed_at,offered_tools_json FROM model_turns WHERE turn_id=?`, turnID)
	record, _, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelResponse{}, "", ErrTurnNotFound
	}
	if err != nil {
		return ModelResponse{}, "", errors.New("model turn read failed")
	}
	if record.Status != StatusResponded && record.Status != StatusConsumed {
		return ModelResponse{}, record.Status, ErrTurnConflict
	}
	var payload []byte
	if err := s.db.QueryRowContext(ctx, `SELECT content FROM turn_bodies WHERE body_ref=? AND kind='response' AND expires_at>?`, record.ResponseRef, s.now().UTC().UnixNano()).Scan(&payload); err != nil {
		return ModelResponse{}, record.Status, errors.New("model response body unavailable")
	}
	return ModelResponse{
		RuntimeID:     record.RuntimeID,
		TurnID:        record.TurnID,
		Sequence:      record.Sequence,
		RequestDigest: record.RequestDigest,
		Payload:       json.RawMessage(payload),
	}, record.Status, nil
}
