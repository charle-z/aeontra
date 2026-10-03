package edge

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// LatestDevelopmentProcessOperation reads the latest exact status/stop intent
// from the existing journal. It creates no operation, recovers no lease and
// grants no new command authority. A coordinator consumes the last completed
// observation before scheduling a new read, including after its own restart.
func (s *Store) LatestDevelopmentProcessOperation(deviceID string, kind OperationKind, request OperationRequest) (Operation, bool, error) {
	if s == nil || s.db == nil || !idPattern.MatchString(deviceID) || (kind != OperationProjectProcessStatus && kind != OperationProjectProcessStop) {
		return Operation{}, false, errors.New("development process observation lookup is invalid")
	}
	normalized, err := validateOperationRequestWithProjectExec(kind, request)
	if err != nil {
		return Operation{}, false, err
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return Operation{}, false, errors.New("development process observation lookup is invalid")
	}
	sum := sha256.Sum256(body)
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.operationLifecycleByDigest(deviceID, kind, hex.EncodeToString(sum[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, errors.New("development process observation is unavailable")
	}
	if op.DeviceID != deviceID || op.Kind != kind || !operationRequestsEqual(op.Request, normalized) {
		return Operation{}, false, errors.New("development process observation identity mismatch")
	}
	return op, true, nil
}
