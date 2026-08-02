package edge

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	maxOperationProgressBytes = 1024
	operationHeartbeatTTL     = time.Minute
)

func (s *Store) recoverExpiredOperationLeasesForDeviceLocked(deviceID string) error {
	return recoverExpiredOperationLeases(s.db, s.now(), "device_id", deviceID)
}

func (s *Store) recoverExpiredOperationLeaseByIDLocked(operationID string) error {
	return recoverExpiredOperationLeases(s.db, s.now(), "operation_id", operationID)
}

func (s *Store) ReportOperationProgress(deviceID, operationID, leaseID string, progress OperationProgress) (OperationControl, error) {
	body, _ := json.Marshal(progress)
	if !idPattern.MatchString(deviceID) || !operationIDPattern.MatchString(operationID) || !leaseIDPattern.MatchString(leaseID) ||
		!validOperationProgress(progress) || len(body) > maxOperationProgressBytes {
		return OperationControl{}, errors.New("operation progress is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	expires := now.Add(operationHeartbeatTTL)
	var currentBody []byte
	var cancelRequested bool
	if err := s.db.QueryRow(`SELECT progress_json,cancel_requested FROM edge_operations WHERE operation_id=? AND device_id=? AND lease_id=? AND state=? AND lease_until>?`, operationID, deviceID, leaseID, OperationLeased, now.UnixNano()).Scan(&currentBody, &cancelRequested); err != nil {
		return OperationControl{}, errors.New("active operation lease not found")
	}
	if len(currentBody) > 0 {
		var current OperationProgress
		if len(currentBody) > maxOperationProgressBytes || json.Unmarshal(currentBody, &current) != nil || !validOperationProgress(current) || progress.Revision <= current.Revision {
			return OperationControl{}, errors.New("operation progress revision is invalid")
		}
	}
	updated, err := s.db.Exec(`UPDATE edge_operations SET progress_json=?,lease_until=?,updated_at=? WHERE operation_id=? AND device_id=? AND lease_id=? AND state=? AND lease_until>?`, body, expires.UnixNano(), now.UnixNano(), operationID, deviceID, leaseID, OperationLeased, now.UnixNano())
	if err != nil {
		return OperationControl{}, errors.New("operation progress unavailable")
	}
	rows, _ := updated.RowsAffected()
	if rows != 1 {
		return OperationControl{}, errors.New("active operation lease not found")
	}
	return OperationControl{CancelRequested: cancelRequested}, nil
}

func (s *Store) RequestOperationCancel(operationID string) (Operation, error) {
	if !operationIDPattern.MatchString(operationID) {
		return Operation{}, errors.New("operation id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverExpiredOperationLeaseByIDLocked(operationID); err != nil {
		return Operation{}, errors.New("operation cancellation unavailable")
	}
	var state OperationState
	var kind OperationKind
	if err := s.db.QueryRow(`SELECT state,kind FROM edge_operations WHERE operation_id=?`, operationID).Scan(&state, &kind); err != nil {
		return Operation{}, errors.New("edge operation not found")
	}
	if state == OperationCancelled {
		return s.operationLifecycleByID(operationID)
	}
	if state != OperationQueued && state != OperationLeased {
		return Operation{}, errors.New("cancellable edge operation not found")
	}
	if state == OperationLeased && !operationKindInterruptible(kind) {
		return Operation{}, errors.New("running edge operation is not cancellable")
	}
	now := s.now().UTC().UnixNano()
	updated, err := s.db.Exec(`UPDATE edge_operations SET cancel_requested=1,state=CASE WHEN state=? THEN ? ELSE state END,safe_code=CASE WHEN state=? THEN 'operation_cancelled' ELSE safe_code END,updated_at=? WHERE operation_id=? AND state IN (?,?)`, OperationQueued, OperationCancelled, OperationQueued, now, operationID, OperationQueued, OperationLeased)
	if err != nil {
		return Operation{}, errors.New("operation cancellation failed")
	}
	rows, _ := updated.RowsAffected()
	if rows != 1 {
		return Operation{}, errors.New("cancellable edge operation not found")
	}
	return s.operationLifecycleByID(operationID)
}

func (s *Store) CancelLeasedOperation(deviceID, operationID, leaseID string) (Operation, error) {
	if !idPattern.MatchString(deviceID) || !operationIDPattern.MatchString(operationID) || !leaseIDPattern.MatchString(leaseID) {
		return Operation{}, errors.New("operation cancellation acknowledgement is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	updated, err := s.db.Exec(`UPDATE edge_operations SET state=?,cancel_requested=1,safe_code='operation_cancelled',lease_id=NULL,lease_until=NULL,updated_at=? WHERE operation_id=? AND device_id=? AND lease_id=? AND state=? AND lease_until>?`, OperationCancelled, now.UnixNano(), operationID, deviceID, leaseID, OperationLeased, now.UnixNano())
	if err != nil {
		return Operation{}, errors.New("operation cancellation acknowledgement failed")
	}
	rows, _ := updated.RowsAffected()
	if rows != 1 {
		return Operation{}, errors.New("active operation lease not found")
	}
	return s.operationLifecycleByID(operationID)
}

func (s *Store) ActiveOperations(deviceID string, limit int) ([]Operation, error) {
	if !idPattern.MatchString(deviceID) || limit < 1 || limit > 100 {
		return nil, errors.New("active operation list request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverExpiredOperationLeasesForDeviceLocked(deviceID); err != nil {
		return nil, errors.New("active operation list unavailable")
	}
	rows, err := s.db.Query(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,cancel_requested,progress_json,created_at,updated_at FROM edge_operations WHERE device_id=? AND state IN (?,?) ORDER BY created_at,operation_id LIMIT ?`, deviceID, OperationQueued, OperationLeased, limit)
	if err != nil {
		return nil, errors.New("active operation list unavailable")
	}
	defer rows.Close()
	operations := make([]Operation, 0)
	for rows.Next() {
		op, err := scanOperationLifecycle(rows)
		if err != nil {
			return nil, errors.New("active operation list unavailable")
		}
		operations = append(operations, op)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("active operation list unavailable")
	}
	return operations, nil
}

func (s *Store) OperationLifecycleStatus(operationID string) (Operation, error) {
	if !operationIDPattern.MatchString(operationID) {
		return Operation{}, errors.New("operation id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverExpiredOperationLeaseByIDLocked(operationID); err != nil {
		return Operation{}, errors.New("edge operation unavailable")
	}
	op, err := s.operationLifecycleByID(operationID)
	if err != nil {
		return Operation{}, errors.New("edge operation not found")
	}
	return op, nil
}

func OperationCanCancel(operation Operation) bool {
	switch operation.State {
	case OperationQueued:
		return true
	case OperationLeased:
		return operationKindInterruptible(operation.Kind)
	default:
		return false
	}
}

func operationKindInterruptible(kind OperationKind) bool {
	switch kind {
	case OperationBundleUpdate, OperationBundleRollback, OperationEdgeRepair:
		return false
	default:
		return true
	}
}

func (s *Store) operationLifecycleByDigest(deviceID string, kind OperationKind, digest string) (Operation, error) {
	return scanOperationLifecycle(s.db.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,cancel_requested,progress_json,created_at,updated_at FROM edge_operations WHERE device_id=? AND kind=? AND request_digest=? ORDER BY created_at DESC LIMIT 1`, deviceID, kind, digest))
}

func (s *Store) operationLifecycleByID(operationID string) (Operation, error) {
	return scanOperationLifecycle(s.db.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,cancel_requested,progress_json,created_at,updated_at FROM edge_operations WHERE operation_id=?`, operationID))
}

func validOperationProgress(progress OperationProgress) bool {
	if progress.Revision == 0 || !operationProgressPhasePattern.MatchString(progress.Phase) || progress.TotalUnits > MaxOperationProgressUnits {
		return false
	}
	if progress.TotalUnits == 0 {
		return progress.CompletedUnits == 0
	}
	return progress.CompletedUnits <= progress.TotalUnits
}

func scanOperationLifecycle(row rowScanner) (Operation, error) {
	var op Operation
	var request, result, progress []byte
	var created, updated int64
	if err := row.Scan(&op.ID, &op.DeviceID, &op.Kind, &request, &op.State, &result, &op.SafeCode, &op.CancelRequested, &progress, &created, &updated); err != nil {
		return Operation{}, err
	}
	if json.Unmarshal(request, &op.Request) != nil || len(result) > MaxOperationResultBytes ||
		(len(result) > 0 && json.Unmarshal(result, &op.Result) != nil) || len(progress) > maxOperationProgressBytes ||
		(len(progress) > 0 && (json.Unmarshal(progress, &op.Progress) != nil || !validOperationProgress(op.Progress))) {
		return Operation{}, errors.New("stored operation is invalid")
	}
	normalized, err := validateOperationRequestWithProjectExec(op.Kind, op.Request)
	if err != nil || !operationRequestsEqual(normalized, op.Request) || !validStoredOperationLifecycle(op) {
		return Operation{}, errors.New("stored operation is invalid")
	}
	op.CreatedAt = time.Unix(0, created).UTC()
	op.UpdatedAt = time.Unix(0, updated).UTC()
	return op, nil
}

func validStoredOperationLifecycle(operation Operation) bool {
	switch operation.State {
	case OperationQueued:
		return !operation.CancelRequested && operation.SafeCode == "" && emptyOperationResult(operation.Result)
	case OperationLeased:
		return operation.SafeCode == "" && emptyOperationResult(operation.Result)
	case OperationSucceeded:
		return !operation.CancelRequested && operation.SafeCode == "" && validOperationCompletion(operation.Result, "")
	case OperationFailed:
		return !operation.CancelRequested && operation.SafeCode != "" && validOperationCompletion(operation.Result, operation.SafeCode)
	case OperationCancelled:
		return operation.CancelRequested && operation.SafeCode == "operation_cancelled" && emptyOperationResult(operation.Result)
	default:
		return false
	}
}
