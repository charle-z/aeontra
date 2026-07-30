package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	MaxOperationProgressUnits = 1_000_000_000
	MaxOperationResultBytes   = 64 << 10
)

const (
	OperationLabPrepare       OperationKind = "lab_prepare"
	OperationLabRetarget      OperationKind = "lab_retarget"
	OperationAutopilotStart   OperationKind = "autopilot_start"
	OperationAutopilotPause   OperationKind = "autopilot_pause"
	OperationAutopilotResume  OperationKind = "autopilot_resume"
	OperationAutopilotCancel  OperationKind = "autopilot_cancel"
	OperationBundleStatus     OperationKind = "bundle_status"
	OperationBundleUpdate     OperationKind = "bundle_update"
	OperationBundleRollback   OperationKind = "bundle_rollback"
	OperationEdgeRepair       OperationKind = "edge_repair"
	OperationOnboardingStatus OperationKind = "onboarding_status"
	OperationProjectPrepare   OperationKind = "project_prepare"
	OperationProjectStatus    OperationKind = "project_status"
	OperationProjectSnapshot  OperationKind = "project_snapshot"

	OperationQueued    OperationState = "queued"
	OperationLeased    OperationState = "leased"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
	OperationCancelled OperationState = "cancelled"
)

var operationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
var projectOperationAliasPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var projectOperationTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
var projectOperationRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
var projectOperationIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var operationProgressPhasePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

type OperationRequest struct {
	Platform        string `json:"platform,omitempty"`
	Machine         string `json:"machine,omitempty"`
	Target          string `json:"target"`
	Difficulty      string `json:"difficulty,omitempty"`
	OperatingSystem string `json:"operating_system,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	RunUntil        string `json:"run_until,omitempty"`
	Release         string `json:"release,omitempty"`
	Alias           string `json:"alias,omitempty"`
	Repository      string `json:"repository,omitempty"`
	TargetAlias     string `json:"target_alias,omitempty"`
	Profile         string `json:"profile,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type OperationResult struct {
	WorkspaceID           string   `json:"workspace_id,omitempty"`
	AuthorizationRevision uint64   `json:"authorization_revision,omitempty"`
	JobID                 string   `json:"job_id,omitempty"`
	JobState              string   `json:"job_state,omitempty"`
	ProgressRevision      uint64   `json:"progress_revision,omitempty"`
	CycleCount            uint64   `json:"cycle_count,omitempty"`
	JobSafeCode           string   `json:"job_safe_code,omitempty"`
	Release               string   `json:"release,omitempty"`
	Commit                string   `json:"commit,omitempty"`
	ManifestStatus        string   `json:"manifest_status,omitempty"`
	ComponentsCompatible  bool     `json:"components_compatible,omitempty"`
	ServiceActive         bool     `json:"service_active,omitempty"`
	ServiceState          string   `json:"service_state,omitempty"`
	ProcessState          string   `json:"process_state,omitempty"`
	LockState             string   `json:"lock_state,omitempty"`
	Coherence             string   `json:"coherence,omitempty"`
	ProcessRelease        string   `json:"process_release,omitempty"`
	ProcessCommit         string   `json:"process_commit,omitempty"`
	UpdateAvailable       bool     `json:"update_available"`
	Paired                bool     `json:"paired,omitempty"`
	BubblewrapValid       bool     `json:"bubblewrap_valid,omitempty"`
	RootlessValid         bool     `json:"rootless_valid,omitempty"`
	WorkspaceCount        int      `json:"workspace_count,omitempty"`
	ProviderValid         bool     `json:"provider_valid,omitempty"`
	DriverValid           bool     `json:"driver_valid,omitempty"`
	Blockers              []string `json:"blockers,omitempty"`
	ProjectAlias          string   `json:"project_alias,omitempty"`
	ProjectOwner          string   `json:"project_owner,omitempty"`
	ProjectRepository     string   `json:"project_repository,omitempty"`
	ProjectTarget         string   `json:"project_target,omitempty"`
	ProjectState          string   `json:"project_state,omitempty"`
	ProjectProfile        string   `json:"project_profile,omitempty"`
	ProjectMode           string   `json:"project_mode,omitempty"`
	SnapshotBranch        string   `json:"snapshot_branch,omitempty"`
	SnapshotHead          string   `json:"snapshot_head,omitempty"`
	SnapshotClean         bool     `json:"snapshot_clean,omitempty"`
}

type OperationProgress struct {
	Revision       uint64 `json:"revision"`
	Phase          string `json:"phase"`
	CompletedUnits uint64 `json:"completed_units,omitempty"`
	TotalUnits     uint64 `json:"total_units,omitempty"`
}

type OperationControl struct {
	CancelRequested bool `json:"cancel_requested"`
}

type Operation struct {
	ID              string            `json:"operation_id"`
	DeviceID        string            `json:"device_id"`
	Kind            OperationKind     `json:"kind"`
	Request         OperationRequest  `json:"request"`
	State           OperationState    `json:"state"`
	Result          OperationResult   `json:"result,omitempty"`
	SafeCode        string            `json:"safe_code,omitempty"`
	Progress        OperationProgress `json:"progress,omitempty"`
	CancelRequested bool              `json:"cancel_requested,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type OperationLease struct {
	Operation        Operation `json:"operation"`
	LeaseID          string    `json:"lease_id"`
	ExpiresAt        time.Time `json:"lease_expires_at"`
	ControlSignature string    `json:"control_signature"`
}

func (lease OperationLease) ControlCanonical() []byte {
	request, _ := json.Marshal(lease.Operation.Request)
	sum := sha256.Sum256(request)
	return []byte(strings.Join([]string{
		"edge-control-v1", lease.Operation.ID, lease.Operation.DeviceID, string(lease.Operation.Kind),
		hex.EncodeToString(sum[:]), lease.LeaseID, lease.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}, "\n"))
}

func (s *Store) SignOperationLease(lease OperationLease) (OperationLease, error) {
	if len(s.controlPrivateKey) != ed25519.PrivateKeySize || lease.Operation.State != OperationLeased || !operationIDPattern.MatchString(lease.Operation.ID) || !leaseIDPattern.MatchString(lease.LeaseID) {
		return OperationLease{}, errors.New("operation lease signing failed")
	}
	lease.ControlSignature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.controlPrivateKey, lease.ControlCanonical()))
	return lease, nil
}

func (s *Store) CreateOperation(deviceID string, kind OperationKind, request OperationRequest) (Operation, bool, error) {
	request, err := validateOperationRequest(kind, request)
	if !idPattern.MatchString(deviceID) || err != nil {
		return Operation{}, false, errors.New("edge operation is invalid")
	}
	body, _ := json.Marshal(request)
	digestInput := body
	if kind == OperationProjectSnapshot {
		digestInput = []byte(request.IdempotencyKey)
	}
	sum := sha256.Sum256(digestInput)
	digest := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverExpiredOperationLeasesForDeviceLocked(deviceID); err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed")
	}
	var state State
	if err := s.db.QueryRow(`SELECT state FROM devices WHERE device_id=?`, deviceID).Scan(&state); err != nil || state != StateActive {
		return Operation{}, false, errors.New("active edge device not found")
	}
	if existing, err := s.operationLifecycleByDigest(deviceID, kind, digest); err == nil {
		if kind == OperationProjectSnapshot && existing.Request != request {
			return Operation{}, false, errors.New("edge operation idempotency conflict")
		}
		if kind == OperationLabPrepare || kind == OperationLabRetarget || kind == OperationProjectSnapshot || existing.State == OperationQueued || existing.State == OperationLeased {
			return existing, false, nil
		}
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
	_, _ = tx.Exec(`UPDATE edge_operations SET state=?,safe_code='operation_cancelled',lease_id=NULL,lease_until=NULL,updated_at=? WHERE device_id=? AND state=? AND cancel_requested=1 AND lease_until<=?`, OperationCancelled, now.UnixNano(), deviceID, OperationLeased, now.UnixNano())
	_, _ = tx.Exec(`UPDATE edge_operations SET state=?,progress_json=NULL,lease_id=NULL,lease_until=NULL,updated_at=? WHERE device_id=? AND state=? AND cancel_requested=0 AND lease_until<=?`, OperationQueued, now.UnixNano(), deviceID, OperationLeased, now.UnixNano())
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
	updated, err := s.db.Exec(`UPDATE edge_operations SET state=?,result_json=?,safe_code=?,lease_id=NULL,lease_until=NULL,updated_at=? WHERE operation_id=? AND device_id=? AND lease_id=? AND state=? AND cancel_requested=0 AND lease_until>?`, state, body, safeCode, now.UnixNano(), operationID, deviceID, leaseID, OperationLeased, now.UnixNano())
	if err != nil {
		return Operation{}, errors.New("operation completion unavailable")
	}
	rows, _ := updated.RowsAffected()
	if rows != 1 {
		return Operation{}, errors.New("active operation lease not found")
	}
	return s.operationLifecycleByID(operationID)
}

func (s *Store) OperationStatus(operationID string) (Operation, error) {
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

func (s *Store) WaitOperation(ctx context.Context, operationID string, timeout time.Duration) (Operation, error) {
	if timeout <= 0 || timeout > 3*time.Minute {
		return Operation{}, errors.New("operation wait is invalid")
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		operation, err := s.OperationStatus(operationID)
		if err != nil {
			return Operation{}, err
		}
		if operation.State == OperationSucceeded || operation.State == OperationFailed || operation.State == OperationCancelled {
			return operation, nil
		}
		select {
		case <-ctx.Done():
			return operation, ctx.Err()
		case <-deadline.C:
			return operation, nil
		case <-ticker.C:
		}
	}
}

func validateOperationRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if kind != OperationProjectSnapshot && request.IdempotencyKey != "" {
		return OperationRequest{}, errors.New("operation idempotency key is invalid")
	}
	switch kind {
	case OperationLabPrepare:
		parsed := net.ParseIP(strings.TrimSpace(request.Target))
		if parsed == nil || parsed.To4() == nil || !parsed.IsPrivate() || strings.Contains(request.Target, "/") {
			return OperationRequest{}, errors.New("target is invalid")
		}
		request.Target = parsed.To4().String()
		request.Platform = strings.ToLower(strings.TrimSpace(request.Platform))
		request.Difficulty = strings.ToLower(strings.TrimSpace(request.Difficulty))
		request.OperatingSystem = strings.ToLower(strings.TrimSpace(request.OperatingSystem))
		request.Machine = strings.TrimSpace(request.Machine)
		if request.Platform != "htb" || !namePattern.MatchString(strings.ToLower(request.Machine)) || len(request.Machine) > 64 || (request.Difficulty != "easy" && request.Difficulty != "medium" && request.Difficulty != "hard") || request.OperatingSystem != "linux" || request.WorkspaceID != "" {
			return OperationRequest{}, errors.New("prepare request is invalid")
		}
	case OperationLabRetarget:
		parsed := net.ParseIP(strings.TrimSpace(request.Target))
		if parsed == nil || parsed.To4() == nil || !parsed.IsPrivate() || strings.Contains(request.Target, "/") {
			return OperationRequest{}, errors.New("target is invalid")
		}
		request.Target = parsed.To4().String()
		if !workspaceIDPattern.MatchString(request.WorkspaceID) || request.Platform != "" || request.Machine != "" || request.Difficulty != "" || request.OperatingSystem != "" {
			return OperationRequest{}, errors.New("retarget request is invalid")
		}
	case OperationAutopilotStart:
		if !workspaceIDPattern.MatchString(request.WorkspaceID) || request.RunUntil != "completed_or_cancelled" || request.Target != "" || request.Platform != "" || request.Machine != "" || request.Difficulty != "" || request.OperatingSystem != "" {
			return OperationRequest{}, errors.New("autopilot start request is invalid")
		}
	case OperationAutopilotPause, OperationAutopilotResume, OperationAutopilotCancel:
		if !workspaceIDPattern.MatchString(request.WorkspaceID) || request.RunUntil != "" || request.Target != "" || request.Platform != "" || request.Machine != "" || request.Difficulty != "" || request.OperatingSystem != "" {
			return OperationRequest{}, errors.New("autopilot control request is invalid")
		}
	case OperationProjectPrepare:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.Repository = strings.TrimSpace(request.Repository)
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		if !validProjectOperationRequestCommon(request) || !projectOperationRepositoryPattern.MatchString(request.Repository) ||
			strings.ContainsAny(request.Repository, `/\\`) || strings.HasPrefix(request.Repository, ".") {
			return OperationRequest{}, errors.New("project prepare request is invalid")
		}
	case OperationProjectStatus:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		if !validProjectOperationRequestCommon(request) || request.Repository != "" {
			return OperationRequest{}, errors.New("project status request is invalid")
		}
	case OperationProjectSnapshot:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
		if !validProjectOperationRequestCommon(request) || request.Repository != "" ||
			!projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
			return OperationRequest{}, errors.New("project snapshot request is invalid")
		}
	case OperationBundleStatus, OperationBundleRollback, OperationEdgeRepair, OperationOnboardingStatus:
		if request != (OperationRequest{}) {
			return OperationRequest{}, errors.New("edge diagnostic request is invalid")
		}
	case OperationBundleUpdate:
		if request != (OperationRequest{Release: "stable"}) {
			return OperationRequest{}, errors.New("bundle update request is invalid")
		}
	default:
		return OperationRequest{}, errors.New("operation kind is invalid")
	}
	return request, nil
}

func validProjectOperationRequestCommon(request OperationRequest) bool {
	return projectOperationAliasPattern.MatchString(request.Alias) && projectOperationTargetPattern.MatchString(request.TargetAlias) &&
		request.Profile == "linux-workcell" && request.Platform == "" && request.Machine == "" && request.Target == "" &&
		request.Difficulty == "" && request.OperatingSystem == "" && request.WorkspaceID == "" && request.RunUntil == "" && request.Release == ""
}

func validOperationCompletion(result OperationResult, code string) bool {
	body, err := json.Marshal(result)
	if err != nil || len(body) > MaxOperationResultBytes {
		return false
	}
	if result.SnapshotBranch != "" || result.SnapshotHead != "" || result.SnapshotClean {
		return code == "" && validProjectSnapshotResult(result)
	}
	if code != "" {
		return regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(code) && emptyOperationResult(result)
	}
	if validProjectSnapshotResult(result) {
		return true
	}
	if validProjectOperationResult(result) {
		return true
	}
	if !workspaceIDPattern.MatchString(result.WorkspaceID) {
		validBundle := regexp.MustCompile(`^p15\.[0-9]+\.[0-9]+$`).MatchString(result.Release) && regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(result.Commit) && result.ManifestStatus == "valid" && result.ComponentsCompatible && result.ProviderValid && result.DriverValid
		invalidComponents := !result.ProviderValid && (!result.DriverValid || result.ManifestStatus == "provider_outdated")
		invalidBundle := result.Release == "" && result.Commit == "" && regexp.MustCompile(`^(bundle_mismatch|provider_outdated|driver_outdated|manifest_invalid)$`).MatchString(result.ManifestStatus) && !result.ComponentsCompatible && invalidComponents
		return (validBundle || invalidBundle) && validDiagnosticBlockers(result.Blockers) && validRuntimeDiagnostic(result)
	}
	if result.JobID != "" {
		return regexp.MustCompile(`^aj_[a-f0-9]{32}$`).MatchString(result.JobID) && regexp.MustCompile(`^(running|paused|blocked|completed|cancelled)$`).MatchString(result.JobState) && (result.JobSafeCode == "" || regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(result.JobSafeCode))
	}
	return result.AuthorizationRevision > 0
}

func validProjectOperationResult(result OperationResult) bool {
	if !workspaceIDPattern.MatchString(result.WorkspaceID) ||
		!projectOperationAliasPattern.MatchString(result.ProjectAlias) || !githubOwnerOperationPattern.MatchString(result.ProjectOwner) ||
		!projectOperationRepositoryPattern.MatchString(result.ProjectRepository) || strings.ContainsAny(result.ProjectRepository, `/\\`) ||
		!projectOperationTargetPattern.MatchString(result.ProjectTarget) || result.ProjectState != "ready" ||
		result.ProjectProfile != "linux-workcell" || result.ProjectMode != "dev" {
		return false
	}
	metadata := result
	metadata.WorkspaceID = ""
	metadata.ProjectAlias = ""
	metadata.ProjectOwner = ""
	metadata.ProjectRepository = ""
	metadata.ProjectTarget = ""
	metadata.ProjectState = ""
	metadata.ProjectProfile = ""
	metadata.ProjectMode = ""
	return emptyOperationResult(metadata)
}

var projectSnapshotBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
var projectSnapshotCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func validProjectSnapshotResult(result OperationResult) bool {
	branch := result.SnapshotBranch
	if !result.SnapshotClean || !projectSnapshotBranchPattern.MatchString(branch) ||
		strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.Contains(branch, "//") ||
		strings.Contains(branch, "@{") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") ||
		strings.HasSuffix(branch, ".lock") || !projectSnapshotCommitPattern.MatchString(result.SnapshotHead) {
		return false
	}
	metadata := result
	metadata.SnapshotBranch = ""
	metadata.SnapshotHead = ""
	metadata.SnapshotClean = false
	return validProjectOperationResult(metadata)
}

var githubOwnerOperationPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func validDiagnosticBlockers(blockers []string) bool {
	if len(blockers) > 16 {
		return false
	}
	for _, blocker := range blockers {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(blocker) {
			return false
		}
	}
	return true
}

func validRuntimeDiagnostic(result OperationResult) bool {
	legacy := result.ServiceState == "" && result.ProcessState == "" && result.LockState == "" && result.Coherence == "" && result.ProcessRelease == "" && result.ProcessCommit == ""
	if legacy {
		return true
	}
	if !regexp.MustCompile(`^(active|inactive|activating|deactivating|failed|reloading|maintenance)$`).MatchString(result.ServiceState) ||
		!regexp.MustCompile(`^(inactive|single|duplicate|incoherent)$`).MatchString(result.ProcessState) ||
		!regexp.MustCompile(`^(missing|held|stale_recoverable|held_unverified|held_incoherent|unlocked_invalid|unlocked_live_owner|blocked)$`).MatchString(result.LockState) ||
		!regexp.MustCompile(`^(stopped|managed|manual|duplicate|incoherent)$`).MatchString(result.Coherence) {
		return false
	}
	if result.ServiceActive != (result.ServiceState == "active") {
		return false
	}
	return validRuntimeDiagnosticState(result)
}

func emptyOperationResult(result OperationResult) bool {
	return result.WorkspaceID == "" && result.AuthorizationRevision == 0 && result.JobID == "" && result.JobState == "" && result.ProgressRevision == 0 && result.CycleCount == 0 && result.JobSafeCode == "" && result.Release == "" && result.Commit == "" && result.ManifestStatus == "" && !result.ComponentsCompatible && !result.ServiceActive && result.ServiceState == "" && result.ProcessState == "" && result.LockState == "" && result.Coherence == "" && result.ProcessRelease == "" && result.ProcessCommit == "" && !result.UpdateAvailable && !result.Paired && !result.BubblewrapValid && !result.RootlessValid && result.WorkspaceCount == 0 && !result.ProviderValid && !result.DriverValid && len(result.Blockers) == 0 && result.ProjectAlias == "" && result.ProjectOwner == "" && result.ProjectRepository == "" && result.ProjectTarget == "" && result.ProjectState == "" && result.ProjectProfile == "" && result.ProjectMode == ""
}

func (s *Store) AutopilotStatus(workspaceID string) (OperationResult, error) {
	if !workspaceIDPattern.MatchString(workspaceID) {
		return OperationResult{}, errors.New("workspace id is invalid")
	}
	var body []byte
	if err := s.db.QueryRow(`SELECT result_json FROM edge_autopilot_status WHERE workspace_id=?`, workspaceID).Scan(&body); err != nil {
		return OperationResult{}, errors.New("autopilot job not found")
	}
	var result OperationResult
	if json.Unmarshal(body, &result) != nil || result.WorkspaceID != workspaceID || !validOperationCompletion(result, "") {
		return OperationResult{}, errors.New("autopilot status unavailable")
	}
	return result, nil
}

func (s *Store) ReportAutopilot(deviceID string, result OperationResult) error {
	if !idPattern.MatchString(deviceID) || !validOperationCompletion(result, "") || result.JobID == "" {
		return errors.New("autopilot report is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var bound string
	if err := s.db.QueryRow(`SELECT device_id FROM edge_workspaces WHERE workspace_id=?`, result.WorkspaceID).Scan(&bound); err != nil || bound != deviceID {
		return errors.New("autopilot workspace is not registered")
	}
	body, _ := json.Marshal(result)
	_, err := s.db.Exec(`INSERT INTO edge_autopilot_status(workspace_id,device_id,result_json,updated_at) VALUES(?,?,?,?) ON CONFLICT(workspace_id) DO UPDATE SET device_id=excluded.device_id,result_json=excluded.result_json,updated_at=excluded.updated_at`, result.WorkspaceID, deviceID, body, s.now().UTC().UnixNano())
	if err != nil {
		return errors.New("autopilot report unavailable")
	}
	return nil
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
