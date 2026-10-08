package workqueue

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	// MaxDevelopmentRequests bounds the number of concurrently retained
	// nonterminal records; expired terminal key bindings are purged separately.
	MaxDevelopmentRequests           = 1024
	MaxDevelopmentRequestRecordBytes = 4096
	developmentRequestDigestDomain   = "aeontra-development-request-v1\x00"
	maxDevelopmentRequestRevision    = uint64(1 << 20)
	// DevelopmentRequestIdempotencyTTL retains terminal key bindings for thirty days.
	// After expiry, a new request may reuse that key with a new initial binding.
	DevelopmentRequestIdempotencyTTL = 30 * 24 * time.Hour
)

var (
	developmentRequestIDPattern = regexp.MustCompile(`^dr_[a-f0-9]{32}$`)
	developmentRequestDevice    = regexp.MustCompile(`^ed_[a-f0-9]{32}$`)
	developmentRequestGoal      = regexp.MustCompile(`^mb_[a-f0-9]{32}$`)
	developmentRequestOp        = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
	developmentRequestProcess   = regexp.MustCompile(`^pr_[a-f0-9]{32}$`)
	developmentRequestDigest    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type DevelopmentRequestState string

const (
	DevelopmentRequestPreparing         DevelopmentRequestState = "preparing"
	DevelopmentRequestActive            DevelopmentRequestState = "active"
	DevelopmentRequestCancelling        DevelopmentRequestState = "cancelling"
	DevelopmentRequestCompleted         DevelopmentRequestState = "completed"
	DevelopmentRequestFailed            DevelopmentRequestState = "failed"
	DevelopmentRequestCancelled         DevelopmentRequestState = "cancelled"
	DevelopmentRequestAwaitingReasoning DevelopmentRequestState = "awaiting_reasoning"
)

type DevelopmentRequestReason string

const (
	DevelopmentRequestReasonNone                      DevelopmentRequestReason = ""
	DevelopmentRequestReasonInspectionPending         DevelopmentRequestReason = "inspection_pending"
	DevelopmentRequestReasonCapabilityMissing         DevelopmentRequestReason = "capability_missing"
	DevelopmentRequestReasonSourceChanged             DevelopmentRequestReason = "source_changed"
	DevelopmentRequestReasonCodeFailure               DevelopmentRequestReason = "code_failure"
	DevelopmentRequestReasonNewRequirement            DevelopmentRequestReason = "new_requirement"
	DevelopmentRequestReasonOperationFailed           DevelopmentRequestReason = "operation_failed"
	DevelopmentRequestReasonOperationUnknown          DevelopmentRequestReason = "operation_unknown"
	DevelopmentRequestReasonReconciliationRequired    DevelopmentRequestReason = "reconciliation_required"
	DevelopmentRequestReasonSemanticAcceptancePending DevelopmentRequestReason = "semantic_acceptance_pending"
	DevelopmentRequestReasonCancellationRequested     DevelopmentRequestReason = "cancellation_requested"
)

// DevelopmentRequest is private content-free lifecycle metadata. BodyRef and
// BodyDigest identify a pinned staged goal; no goal bytes, argv, command output,
// URL, path or credential are stored in this record.
type DevelopmentRequest struct {
	ID                    string                   `json:"request_id"`
	Revision              uint64                   `json:"revision"`
	KeyDigest             string                   `json:"key_digest"`
	Alias                 string                   `json:"alias"`
	Target                string                   `json:"target"`
	DeviceID              string                   `json:"device_id"`
	BodyRef               string                   `json:"body_ref"`
	BodyDigest            string                   `json:"body_digest"`
	InspectionOperationID string                   `json:"inspection_operation_id,omitempty"`
	ObjectiveID           string                   `json:"objective_id,omitempty"`
	ActiveOperationID     string                   `json:"active_operation_id,omitempty"`
	ProcessID             string                   `json:"process_id,omitempty"`
	State                 DevelopmentRequestState  `json:"state"`
	Reason                DevelopmentRequestReason `json:"reason,omitempty"`
}

func (request DevelopmentRequest) Valid() bool {
	if !developmentRequestIDPattern.MatchString(request.ID) || request.Revision == 0 || request.Revision > maxDevelopmentRequestRevision ||
		!developmentRequestDigest.MatchString(request.KeyDigest) || !taskProjectPattern.MatchString(request.Alias) ||
		!taskTargetPattern.MatchString(request.Target) || !developmentRequestDevice.MatchString(request.DeviceID) ||
		!developmentRequestGoal.MatchString(request.BodyRef) || !developmentRequestDigest.MatchString(request.BodyDigest) ||
		!validDevelopmentRequestState(request.State) || !validDevelopmentRequestReason(request.Reason) {
		return false
	}
	if request.InspectionOperationID != "" && !developmentRequestOp.MatchString(request.InspectionOperationID) ||
		request.ObjectiveID != "" && !validDevelopmentRequestObjectiveID(request.ObjectiveID) ||
		request.ActiveOperationID != "" && !developmentRequestOp.MatchString(request.ActiveOperationID) ||
		request.ProcessID != "" && !developmentRequestProcess.MatchString(request.ProcessID) {
		return false
	}
	if request.State == DevelopmentRequestCancelling && request.Reason == DevelopmentRequestReasonReconciliationRequired && request.ProcessID == "" {
		return false
	}
	return validDevelopmentRequestReasonForState(request.State, request.Reason)
}

// SaveDevelopmentRequest creates revision one or compare-and-swaps one valid
// successor. Reusing KeyDigest with a changed initial binding is a conflict;
// replaying the same initial binding returns the already durable request.
func (s *Store) SaveDevelopmentRequest(request DevelopmentRequest) (DevelopmentRequest, bool, error) {
	if s == nil || s.db == nil || !request.Valid() {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request is invalid")
	}
	body, digest, err := marshalDevelopmentRequest(request)
	if err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request transaction failed")
	}
	defer tx.Rollback()
	now := s.now().UTC()
	if err := pruneExpiredDevelopmentRequests(tx, now); err != nil {
		return DevelopmentRequest{}, false, err
	}
	existing, found, err := developmentRequestByIDTx(tx, request.ID)
	if err != nil {
		return DevelopmentRequest{}, false, err
	}
	if !found {
		byKey, keyFound, err := developmentRequestByKeyTx(tx, request.KeyDigest)
		if err != nil {
			return DevelopmentRequest{}, false, err
		}
		if keyFound {
			if !sameDevelopmentRequestBinding(request, byKey) {
				return DevelopmentRequest{}, false, errors.New("workqueue: development request idempotency key conflicts")
			}
			return byKey, false, nil
		}
		if !validInitialDevelopmentRequest(request) {
			return DevelopmentRequest{}, false, errors.New("workqueue: development request must begin at revision one")
		}
		var activeCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM development_requests WHERE state NOT IN (?,?,?)`,
			DevelopmentRequestCompleted, DevelopmentRequestFailed, DevelopmentRequestCancelled).Scan(&activeCount); err != nil {
			return DevelopmentRequest{}, false, errors.New("workqueue: development request capacity unavailable")
		}
		if activeCount < 0 || activeCount >= MaxDevelopmentRequests {
			return DevelopmentRequest{}, false, errors.New("workqueue: active development request bound exceeded")
		}
		nowUnix := now.UnixNano()
		if _, err := tx.Exec(`INSERT INTO development_requests(request_id,key_digest,revision,state,record_digest,record_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			request.ID, request.KeyDigest, request.Revision, request.State, digest, body, nowUnix, nowUnix); err != nil {
			return DevelopmentRequest{}, false, errors.New("workqueue: development request persistence failed")
		}
		if err := tx.Commit(); err != nil {
			return DevelopmentRequest{}, false, errors.New("workqueue: development request persistence failed")
		}
		return request, true, nil
	}
	if !sameDevelopmentRequestBinding(request, existing) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request identity conflicts")
	}
	existingBody, existingDigest, err := marshalDevelopmentRequest(existing)
	if err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request record is corrupt")
	}
	_ = existingBody
	if request.Revision == existing.Revision && digest == existingDigest {
		return existing, false, nil
	}
	if request.Revision != existing.Revision+1 || !validDevelopmentRequestTransition(existing, request) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request revision conflict")
	}
	nowUnix, err := nextDevelopmentRequestUpdatedAt(tx, request.ID, now.UnixNano())
	if err != nil {
		return DevelopmentRequest{}, false, err
	}
	result, err := tx.Exec(`UPDATE development_requests
		SET revision=?,state=?,record_digest=?,record_json=?,updated_at=?
		WHERE request_id=? AND revision=? AND record_digest=?`,
		request.Revision, request.State, digest, body, nowUnix,
		request.ID, existing.Revision, existingDigest)
	if err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request persistence failed")
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request revision conflict")
	}
	if err := tx.Commit(); err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request persistence failed")
	}
	return request, false, nil
}

func (s *Store) DevelopmentRequest(requestID string) (DevelopmentRequest, bool, error) {
	if s == nil || s.db == nil || !developmentRequestIDPattern.MatchString(requestID) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return developmentRequestByID(s.db, requestID)
}

func (s *Store) DevelopmentRequestByKey(keyDigest string) (DevelopmentRequest, bool, error) {
	if s == nil || s.db == nil || !developmentRequestDigest.MatchString(keyDigest) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request key digest is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	request, found, err := developmentRequestByKey(s.db, keyDigest)
	if err != nil || !found || !developmentRequestTerminal(request.State) {
		return request, found, err
	}
	var updatedAt int64
	if err := s.db.QueryRow(`SELECT updated_at FROM development_requests WHERE key_digest=?`, keyDigest).Scan(&updatedAt); err != nil {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request key lookup failed")
	}
	if updatedAt <= s.now().UTC().Add(-DevelopmentRequestIdempotencyTTL).UnixNano() {
		return DevelopmentRequest{}, false, nil
	}
	return request, true, nil
}

// DevelopmentRequests lists bounded dispatch work, excluding requests waiting
// for user/model reasoning. Pollers must TouchDevelopmentRequest with the
// observed revision before processing so the updated_at order advances fairly.
func (s *Store) DevelopmentRequests(limit int) ([]DevelopmentRequest, error) {
	if s == nil || s.db == nil || limit < 1 || limit > MaxListResults {
		return nil, errors.New("workqueue: development request list limit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return listDevelopmentRequests(s.db, limit, false)
}

// TouchDevelopmentRequest advances one eligible request by CAS without changing
// its binding. Its strictly increasing update time rotates it behind untouched
// work even when the local clock has not advanced between polls.
func (s *Store) TouchDevelopmentRequest(requestID string, expectedRevision uint64) (DevelopmentRequest, error) {
	if s == nil || s.db == nil || !developmentRequestIDPattern.MatchString(requestID) ||
		expectedRevision == 0 || expectedRevision >= maxDevelopmentRequestRevision {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch transaction failed")
	}
	defer tx.Rollback()
	request, found, err := developmentRequestByIDTx(tx, requestID)
	if err != nil || !found || request.Revision != expectedRevision ||
		request.State == DevelopmentRequestAwaitingReasoning || developmentRequestTerminal(request.State) {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch conflict")
	}
	_, oldDigest, err := marshalDevelopmentRequest(request)
	if err != nil {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch record is corrupt")
	}
	request.Revision++
	body, digest, err := marshalDevelopmentRequest(request)
	if err != nil {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch record is invalid")
	}
	updatedAt, err := nextDevelopmentRequestUpdatedAt(tx, request.ID, s.now().UTC().UnixNano())
	if err != nil {
		return DevelopmentRequest{}, err
	}
	result, err := tx.Exec(`UPDATE development_requests
		SET revision=?,record_digest=?,record_json=?,updated_at=?
		WHERE request_id=? AND revision=? AND record_digest=?`,
		request.Revision, digest, body, updatedAt, request.ID, expectedRevision, oldDigest)
	if err != nil {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch persistence failed")
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch conflict")
	}
	if err := tx.Commit(); err != nil {
		return DevelopmentRequest{}, errors.New("workqueue: development request touch persistence failed")
	}
	return request, nil
}

// DevelopmentGoalOwners returns at most MaxDevelopmentRequests active goal
// pins for startup reconciliation with modelturn. Terminal request rows are
// intentionally omitted so their goal bodies can be reclaimed.
func (s *Store) DevelopmentGoalOwners() ([]modelturn.TaskGoalOwner, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("workqueue: development goal owners are unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requests, err := listDevelopmentRequests(s.db, MaxDevelopmentRequests+1, true)
	if err != nil {
		return nil, err
	}
	if len(requests) > MaxDevelopmentRequests {
		return nil, errors.New("workqueue: development goal owner bound exceeded")
	}
	owners := make([]modelturn.TaskGoalOwner, 0, len(requests))
	for _, request := range requests {
		owners = append(owners, modelturn.TaskGoalOwner{
			OwnerDigest: request.KeyDigest,
			References: []modelturn.TaskGoalReference{{
				BodyRef:       request.BodyRef,
				ContentDigest: request.BodyDigest,
			}},
		})
	}
	return owners, nil
}

func validInitialDevelopmentRequest(request DevelopmentRequest) bool {
	return request.Valid() && request.Revision == 1 && request.State == DevelopmentRequestPreparing &&
		request.Reason == DevelopmentRequestReasonNone && request.InspectionOperationID == "" &&
		request.ObjectiveID == "" && request.ActiveOperationID == "" && request.ProcessID == ""
}

func validDevelopmentRequestTransition(before, after DevelopmentRequest) bool {
	if !before.Valid() || !after.Valid() || developmentRequestTerminal(before.State) ||
		before.ID != after.ID || before.KeyDigest != after.KeyDigest || before.Alias != after.Alias ||
		before.Target != after.Target || before.DeviceID != after.DeviceID || before.BodyRef != after.BodyRef ||
		before.BodyDigest != after.BodyDigest || after.Revision != before.Revision+1 ||
		!validDevelopmentRequestStateTransition(before.State, after.State) ||
		!setOnceDevelopmentRequestID(before.InspectionOperationID, after.InspectionOperationID, developmentRequestOp.MatchString) ||
		!setOnceDevelopmentRequestID(before.ObjectiveID, after.ObjectiveID, validDevelopmentRequestObjectiveID) ||
		!setOnceDevelopmentRequestID(before.ProcessID, after.ProcessID, developmentRequestProcess.MatchString) {
		return false
	}
	// Once a failed stop enters captured-process observation, later polls cannot
	// erase that phase and authorize another stop, even if its journal is pruned.
	if before.State == DevelopmentRequestCancelling && before.Reason == DevelopmentRequestReasonReconciliationRequired &&
		after.State == DevelopmentRequestCancelling && after.Reason != DevelopmentRequestReasonReconciliationRequired {
		return false
	}
	return true
}

func setOnceDevelopmentRequestID(before, after string, valid func(string) bool) bool {
	if before == "" {
		return true
	}
	return before == after && valid(after)
}

func validDevelopmentRequestObjectiveID(value string) bool {
	return value == strings.TrimSpace(value) && development.ValidObjectiveID(value)
}

func validDevelopmentRequestStateTransition(before, after DevelopmentRequestState) bool {
	if before == after {
		return true
	}
	switch before {
	case DevelopmentRequestPreparing:
		return after == DevelopmentRequestActive || after == DevelopmentRequestCancelling || after == DevelopmentRequestFailed || after == DevelopmentRequestCancelled
	case DevelopmentRequestActive:
		return after == DevelopmentRequestCancelling || after == DevelopmentRequestCompleted ||
			after == DevelopmentRequestFailed || after == DevelopmentRequestCancelled || after == DevelopmentRequestAwaitingReasoning
	case DevelopmentRequestCancelling:
		return after == DevelopmentRequestCompleted || after == DevelopmentRequestFailed || after == DevelopmentRequestCancelled
	case DevelopmentRequestAwaitingReasoning:
		return after == DevelopmentRequestCancelling || after == DevelopmentRequestCompleted ||
			after == DevelopmentRequestFailed || after == DevelopmentRequestCancelled
	default:
		return false
	}
}

func validDevelopmentRequestState(state DevelopmentRequestState) bool {
	switch state {
	case DevelopmentRequestPreparing, DevelopmentRequestActive, DevelopmentRequestCancelling,
		DevelopmentRequestCompleted, DevelopmentRequestFailed, DevelopmentRequestCancelled,
		DevelopmentRequestAwaitingReasoning:
		return true
	default:
		return false
	}
}

func validDevelopmentRequestReason(reason DevelopmentRequestReason) bool {
	switch reason {
	case DevelopmentRequestReasonNone, DevelopmentRequestReasonInspectionPending,
		DevelopmentRequestReasonCapabilityMissing, DevelopmentRequestReasonSourceChanged, DevelopmentRequestReasonCodeFailure,
		DevelopmentRequestReasonNewRequirement,
		DevelopmentRequestReasonOperationFailed, DevelopmentRequestReasonOperationUnknown,
		DevelopmentRequestReasonReconciliationRequired, DevelopmentRequestReasonSemanticAcceptancePending,
		DevelopmentRequestReasonCancellationRequested:
		return true
	default:
		return false
	}
}

func validDevelopmentRequestReasonForState(state DevelopmentRequestState, reason DevelopmentRequestReason) bool {
	switch state {
	case DevelopmentRequestAwaitingReasoning:
		return reason == DevelopmentRequestReasonSemanticAcceptancePending || reason == DevelopmentRequestReasonCodeFailure ||
			reason == DevelopmentRequestReasonNewRequirement || reason == DevelopmentRequestReasonSourceChanged
	case DevelopmentRequestCancelling:
		return reason == DevelopmentRequestReasonCancellationRequested || reason == DevelopmentRequestReasonReconciliationRequired
	case DevelopmentRequestCancelled:
		return reason == DevelopmentRequestReasonNone || reason == DevelopmentRequestReasonCancellationRequested
	case DevelopmentRequestCompleted:
		return reason == DevelopmentRequestReasonNone
	case DevelopmentRequestFailed:
		return reason != DevelopmentRequestReasonNone && reason != DevelopmentRequestReasonSemanticAcceptancePending &&
			reason != DevelopmentRequestReasonCancellationRequested
	default:
		return reason != DevelopmentRequestReasonSemanticAcceptancePending && reason != DevelopmentRequestReasonCancellationRequested
	}
}

func developmentRequestTerminal(state DevelopmentRequestState) bool {
	return state == DevelopmentRequestCompleted || state == DevelopmentRequestFailed || state == DevelopmentRequestCancelled
}

func sameDevelopmentRequestBinding(left, right DevelopmentRequest) bool {
	return left.KeyDigest == right.KeyDigest && left.Alias == right.Alias && left.Target == right.Target &&
		left.DeviceID == right.DeviceID && left.BodyRef == right.BodyRef && left.BodyDigest == right.BodyDigest
}

func marshalDevelopmentRequest(request DevelopmentRequest) ([]byte, string, error) {
	if !request.Valid() {
		return nil, "", errors.New("workqueue: development request is invalid")
	}
	body, err := json.Marshal(request)
	if err != nil || len(body) == 0 || len(body) > MaxDevelopmentRequestRecordBytes {
		return nil, "", errors.New("workqueue: development request record is unavailable")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(developmentRequestDigestDomain))
	_, _ = hash.Write(body)
	return body, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

type developmentRequestScanner interface {
	Scan(dest ...any) error
}

func developmentRequestByID(db *sql.DB, requestID string) (DevelopmentRequest, bool, error) {
	return scanDevelopmentRequest(db.QueryRow(`SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests WHERE request_id=?`, requestID))
}

func developmentRequestByIDTx(tx *sql.Tx, requestID string) (DevelopmentRequest, bool, error) {
	return scanDevelopmentRequest(tx.QueryRow(`SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests WHERE request_id=?`, requestID))
}

func developmentRequestByKey(db *sql.DB, keyDigest string) (DevelopmentRequest, bool, error) {
	return scanDevelopmentRequest(db.QueryRow(`SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests WHERE key_digest=?`, keyDigest))
}

func developmentRequestByKeyTx(tx *sql.Tx, keyDigest string) (DevelopmentRequest, bool, error) {
	return scanDevelopmentRequest(tx.QueryRow(`SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests WHERE key_digest=?`, keyDigest))
}

func scanDevelopmentRequest(row developmentRequestScanner) (DevelopmentRequest, bool, error) {
	var requestID, state, keyDigest, digest string
	var revision int64
	var body []byte
	if err := row.Scan(&requestID, &revision, &state, &keyDigest, &digest, &body); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DevelopmentRequest{}, false, nil
		}
		return DevelopmentRequest{}, false, errors.New("workqueue: development request read failed")
	}
	if revision < 1 || uint64(revision) > maxDevelopmentRequestRevision || digest == "" ||
		developmentRequestRecordDigest(body) != digest {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request record is corrupt")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request DevelopmentRequest
	if err := decoder.Decode(&request); err != nil || !request.Valid() || request.ID != requestID ||
		request.Revision != uint64(revision) || string(request.State) != state || request.KeyDigest != keyDigest {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request record is corrupt")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request record is corrupt")
	}
	canonicalBody, canonicalDigest, err := marshalDevelopmentRequest(request)
	if err != nil || canonicalDigest != digest || string(canonicalBody) != string(body) {
		return DevelopmentRequest{}, false, errors.New("workqueue: development request record is corrupt")
	}
	return request, true, nil
}

// developmentRequestIntegrity verifies every retained row without exposing its
// body reference or metadata. Store.Integrity calls this while holding s.mu.
func (s *Store) developmentRequestIntegrity() error {
	if s == nil || s.db == nil {
		return errors.New("workqueue: development request integrity unavailable")
	}
	rows, err := s.db.Query(`SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests ORDER BY request_id`)
	if err != nil {
		return errors.New("workqueue: development request integrity scan failed")
	}
	defer rows.Close()
	for rows.Next() {
		if request, found, err := scanDevelopmentRequest(rows); err != nil || !found || !request.Valid() {
			return errors.New("workqueue: development request integrity failed")
		}
	}
	if err := rows.Err(); err != nil {
		return errors.New("workqueue: development request integrity scan failed")
	}
	return nil
}

func listDevelopmentRequests(db *sql.DB, limit int, includeAwaitingReasoning bool) ([]DevelopmentRequest, error) {
	query := `SELECT request_id,revision,state,key_digest,record_digest,record_json
		FROM development_requests WHERE state NOT IN (?,?,?)`
	args := []any{DevelopmentRequestCompleted, DevelopmentRequestFailed, DevelopmentRequestCancelled}
	if !includeAwaitingReasoning {
		query += ` AND state<>?`
		args = append(args, DevelopmentRequestAwaitingReasoning)
	}
	query += ` ORDER BY updated_at ASC,request_id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, errors.New("workqueue: development request list failed")
	}
	defer rows.Close()
	requests := make([]DevelopmentRequest, 0, limit)
	for rows.Next() {
		request, found, err := scanDevelopmentRequest(rows)
		if err != nil || !found {
			return nil, errors.New("workqueue: development request list result is invalid")
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("workqueue: development request list failed")
	}
	return requests, nil
}

func pruneExpiredDevelopmentRequests(tx *sql.Tx, now time.Time) error {
	cutoff := now.Add(-DevelopmentRequestIdempotencyTTL).UnixNano()
	if _, err := tx.Exec(`DELETE FROM development_requests WHERE state IN (?,?,?) AND updated_at<=?`,
		DevelopmentRequestCompleted, DevelopmentRequestFailed, DevelopmentRequestCancelled, cutoff); err != nil {
		return errors.New("workqueue: expired development request pruning failed")
	}
	return nil
}

func nextDevelopmentRequestUpdatedAt(tx *sql.Tx, requestID string, observed int64) (int64, error) {
	var previous int64
	if err := tx.QueryRow(`SELECT updated_at FROM development_requests WHERE request_id=?`, requestID).Scan(&previous); err != nil {
		return 0, errors.New("workqueue: development request update timestamp unavailable")
	}
	if observed <= previous {
		if previous == int64(^uint64(0)>>1) {
			return 0, errors.New("workqueue: development request update timestamp exhausted")
		}
		return previous + 1, nil
	}
	return observed, nil
}

func developmentRequestRecordDigest(body []byte) string {
	if len(body) == 0 || len(body) > MaxDevelopmentRequestRecordBytes {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(developmentRequestDigestDomain))
	_, _ = hash.Write(body)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
