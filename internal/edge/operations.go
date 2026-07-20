package edge

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
)

type OperationKind string
type OperationState string

const (
	OperationLabPrepare  OperationKind = "lab_prepare"
	OperationLabRetarget OperationKind = "lab_retarget"

	OperationQueued    OperationState = "queued"
	OperationLeased    OperationState = "leased"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
)

var operationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)

type OperationRequest struct {
	Platform        string `json:"platform,omitempty"`
	Machine         string `json:"machine,omitempty"`
	Target          string `json:"target"`
	Difficulty      string `json:"difficulty,omitempty"`
	OperatingSystem string `json:"operating_system,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
}

type OperationResult struct {
	WorkspaceID           string `json:"workspace_id,omitempty"`
	AuthorizationRevision uint64 `json:"authorization_revision,omitempty"`
}

type Operation struct {
	ID        string           `json:"operation_id"`
	DeviceID  string           `json:"device_id"`
	Kind      OperationKind    `json:"kind"`
	Request   OperationRequest `json:"request"`
	State     OperationState   `json:"state"`
	Result    OperationResult  `json:"result,omitempty"`
	SafeCode  string           `json:"safe_code,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type OperationLease struct {
	Operation Operation `json:"operation"`
	LeaseID   string    `json:"lease_id"`
	ExpiresAt time.Time `json:"lease_expires_at"`
}

func (s *Store) CreateOperation(deviceID string, kind OperationKind, request OperationRequest) (Operation, bool, error) {
	request, err := validateOperationRequest(kind, request)
	if !idPattern.MatchString(deviceID) || err != nil {
		return Operation{}, false, errors.New("edge operation is invalid")
	}
	body, _ := json.Marshal(request)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	var state State
	if err := s.db.QueryRow(`SELECT state FROM devices WHERE device_id=?`, deviceID).Scan(&state); err != nil || state != StateActive {
		return Operation{}, false, errors.New("active edge device not found")
	}
	if existing, err := s.operationByDigest(deviceID, kind, digest); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, errors.New("edge operation persistence failed")
	}
	id, err := randomOpaque("eo_", 16)
	if err != nil {
		return Operation{}, false, errors.New("edge operation generation failed")
	}
	now := s.now().UTC()
	if _, err := s.db.Exec(`INSERT INTO edge_operations(operation_id,device_id,kind,request_json,request_digest,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, deviceID, kind, body, digest, OperationQueued, now.UnixNano(), now.UnixNano()); err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed")
	}
	return Operation{ID: id, DeviceID: deviceID, Kind: kind, Request: request, State: OperationQueued, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (s *Store) LeaseOperation(deviceID string, ttl time.Duration) (OperationLease, error) {
	if !idPattern.MatchString(deviceID) || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return OperationLease{}, errors.New("operation lease is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	defer tx.Rollback()
	_, _ = tx.Exec(`UPDATE edge_operations SET state=?,lease_id=NULL,lease_until=NULL,updated_at=? WHERE device_id=? AND state=? AND lease_until<=?`, OperationQueued, now.UnixNano(), deviceID, OperationLeased, now.UnixNano())
	var id string
	if err := tx.QueryRow(`SELECT operation_id FROM edge_operations WHERE device_id=? AND state=? ORDER BY created_at,operation_id LIMIT 1`, deviceID, OperationQueued).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OperationLease{}, ErrNoTaskAvailable
		}
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	leaseID, err := randomOpaque("el_", 16)
	if err != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	expires := now.Add(ttl)
	if _, err := tx.Exec(`UPDATE edge_operations SET state=?,lease_id=?,lease_until=?,updated_at=? WHERE operation_id=? AND state=?`, OperationLeased, leaseID, expires.UnixNano(), now.UnixNano(), id, OperationQueued); err != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	op, err := scanOperation(tx.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,created_at,updated_at FROM edge_operations WHERE operation_id=?`, id))
	if err != nil || tx.Commit() != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	return OperationLease{Operation: op, LeaseID: leaseID, ExpiresAt: expires}, nil
}

func (s *Store) CompleteOperation(deviceID, operationID, leaseID string, result OperationResult, safeCode string) (Operation, error) {
	if !idPattern.MatchString(deviceID) || !operationIDPattern.MatchString(operationID) || !leaseIDPattern.MatchString(leaseID) || !validOperationCompletion(result, safeCode) {
		return Operation{}, errors.New("operation completion is invalid")
	}
	state := OperationSucceeded
	if safeCode != "" {
		state = OperationFailed
	}
	body, _ := json.Marshal(result)
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := s.db.Exec(`UPDATE edge_operations SET state=?,result_json=?,safe_code=?,lease_id=NULL,lease_until=NULL,updated_at=? WHERE operation_id=? AND device_id=? AND lease_id=? AND state=? AND lease_until>?`, state, body, safeCode, now.UnixNano(), operationID, deviceID, leaseID, OperationLeased, now.UnixNano())
	if err != nil {
		return Operation{}, errors.New("operation completion unavailable")
	}
	rows, _ := updated.RowsAffected()
	if rows != 1 {
		return Operation{}, errors.New("active operation lease not found")
	}
	return s.operationByID(operationID)
}

func (s *Store) OperationStatus(operationID string) (Operation, error) {
	if !operationIDPattern.MatchString(operationID) {
		return Operation{}, errors.New("operation id is invalid")
	}
	op, err := s.operationByID(operationID)
	if err != nil {
		return Operation{}, errors.New("edge operation not found")
	}
	return op, nil
}

func validateOperationRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	parsed := net.ParseIP(strings.TrimSpace(request.Target))
	if parsed == nil || parsed.To4() == nil || !parsed.IsPrivate() || strings.Contains(request.Target, "/") {
		return OperationRequest{}, errors.New("target is invalid")
	}
	request.Target = parsed.To4().String()
	switch kind {
	case OperationLabPrepare:
		request.Platform = strings.ToLower(strings.TrimSpace(request.Platform))
		request.Difficulty = strings.ToLower(strings.TrimSpace(request.Difficulty))
		request.OperatingSystem = strings.ToLower(strings.TrimSpace(request.OperatingSystem))
		request.Machine = strings.TrimSpace(request.Machine)
		if request.Platform != "htb" || !namePattern.MatchString(strings.ToLower(request.Machine)) || len(request.Machine) > 64 || (request.Difficulty != "easy" && request.Difficulty != "medium" && request.Difficulty != "hard") || request.OperatingSystem != "linux" || request.WorkspaceID != "" {
			return OperationRequest{}, errors.New("prepare request is invalid")
		}
	case OperationLabRetarget:
		if !workspaceIDPattern.MatchString(request.WorkspaceID) || request.Platform != "" || request.Machine != "" || request.Difficulty != "" || request.OperatingSystem != "" {
			return OperationRequest{}, errors.New("retarget request is invalid")
		}
	default:
		return OperationRequest{}, errors.New("operation kind is invalid")
	}
	return request, nil
}

func validOperationCompletion(result OperationResult, code string) bool {
	if code != "" {
		return regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(code) && result == (OperationResult{})
	}
	return workspaceIDPattern.MatchString(result.WorkspaceID) && result.AuthorizationRevision > 0
}

func (s *Store) operationByDigest(device string, kind OperationKind, digest string) (Operation, error) {
	return scanOperation(s.db.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,created_at,updated_at FROM edge_operations WHERE device_id=? AND kind=? AND request_digest=?`, device, kind, digest))
}
func (s *Store) operationByID(id string) (Operation, error) {
	return scanOperation(s.db.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,created_at,updated_at FROM edge_operations WHERE operation_id=?`, id))
}
func scanOperation(row rowScanner) (Operation, error) {
	var op Operation
	var request, result []byte
	var created, updated int64
	if err := row.Scan(&op.ID, &op.DeviceID, &op.Kind, &request, &op.State, &result, &op.SafeCode, &created, &updated); err != nil {
		return Operation{}, err
	}
	if json.Unmarshal(request, &op.Request) != nil || (len(result) > 0 && json.Unmarshal(result, &op.Result) != nil) {
		return Operation{}, errors.New("stored operation is invalid")
	}
	op.CreatedAt = time.Unix(0, created).UTC()
	op.UpdatedAt = time.Unix(0, updated).UTC()
	return op, nil
}
