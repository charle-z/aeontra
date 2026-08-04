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
	OperationLabPrepare                   OperationKind = "lab_prepare"
	OperationLabRetarget                  OperationKind = "lab_retarget"
	OperationAutopilotStart               OperationKind = "autopilot_start"
	OperationAutopilotPause               OperationKind = "autopilot_pause"
	OperationAutopilotResume              OperationKind = "autopilot_resume"
	OperationAutopilotCancel              OperationKind = "autopilot_cancel"
	OperationBundleStatus                 OperationKind = "bundle_status"
	OperationBundleUpdate                 OperationKind = "bundle_update"
	OperationBundleRollback               OperationKind = "bundle_rollback"
	OperationEdgeRepair                   OperationKind = "edge_repair"
	OperationOnboardingStatus             OperationKind = "onboarding_status"
	OperationProjectPrepare               OperationKind = "project_prepare"
	OperationProjectStatus                OperationKind = "project_status"
	OperationProjectSnapshot              OperationKind = "project_snapshot"
	OperationProjectExec                  OperationKind = "project_exec"
	OperationProjectProcessStart          OperationKind = "project_process_start"
	OperationProjectProcessStatus         OperationKind = "project_process_status"
	OperationProjectProcessStop           OperationKind = "project_process_stop"
	OperationProjectProcessSignal         OperationKind = "project_process_signal"
	OperationProjectProcessList           OperationKind = "project_process_list"
	OperationProjectProcessCleanup        OperationKind = "project_process_cleanup"
	OperationProjectGitStatus             OperationKind = "project_git_status"
	OperationProjectGitFetch              OperationKind = "project_git_fetch"
	OperationProjectGitFastForwardPreview OperationKind = "project_git_fast_forward_preview"
	OperationProjectGitFastForward        OperationKind = "project_git_fast_forward"
	OperationProjectGitHubStatus          OperationKind = "project_github_status"
	OperationProjectToolboxCreate         OperationKind = "project_toolbox_create"
	OperationProjectToolboxStatus         OperationKind = "project_toolbox_status"
	OperationProjectToolboxExec           OperationKind = "project_toolbox_exec"
	OperationProjectToolboxInstall        OperationKind = "project_toolbox_install"
	OperationProjectToolboxCleanup        OperationKind = "project_toolbox_cleanup"
	OperationProjectToolboxRepair         OperationKind = "project_toolbox_repair"
	OperationProjectToolboxServiceStart   OperationKind = "project_toolbox_service_start"
	OperationProjectToolboxServiceStatus  OperationKind = "project_toolbox_service_status"
	OperationProjectToolboxServiceStop    OperationKind = "project_toolbox_service_stop"

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
	Platform            string            `json:"platform,omitempty"`
	Machine             string            `json:"machine,omitempty"`
	Target              string            `json:"target"`
	Difficulty          string            `json:"difficulty,omitempty"`
	OperatingSystem     string            `json:"operating_system,omitempty"`
	WorkspaceID         string            `json:"workspace_id,omitempty"`
	RunUntil            string            `json:"run_until,omitempty"`
	Release             string            `json:"release,omitempty"`
	Alias               string            `json:"alias,omitempty"`
	Repository          string            `json:"repository,omitempty"`
	TargetAlias         string            `json:"target_alias,omitempty"`
	Profile             string            `json:"profile,omitempty"`
	IdempotencyKey      string            `json:"idempotency_key,omitempty"`
	Argv                []string          `json:"argv,omitempty"`
	CWD                 string            `json:"cwd,omitempty"`
	Stdin               string            `json:"stdin,omitempty"`
	Environment         map[string]string `json:"environment,omitempty"`
	TimeoutSeconds      int               `json:"timeout_seconds,omitempty"`
	BackgroundProcessID string            `json:"background_process_id,omitempty"`
	StdoutOffset        int64             `json:"stdout_offset,omitempty"`
	StderrOffset        int64             `json:"stderr_offset,omitempty"`
	OutputLimit         int               `json:"output_limit,omitempty"`
	GraceSeconds        int               `json:"grace_seconds,omitempty"`
	BackgroundSignal    string            `json:"background_signal,omitempty"`
	ProcessLimit        int               `json:"process_limit,omitempty"`
	GitPlanID           string            `json:"git_plan_id,omitempty"`
	ToolboxServiceID    string            `json:"toolbox_service_id,omitempty"`
	ToolboxServiceName  string            `json:"toolbox_service_name,omitempty"`
	ToolboxCPUMillis    int               `json:"toolbox_cpu_millis,omitempty"`
	ToolboxMemoryMiB    int               `json:"toolbox_memory_mib,omitempty"`
	ToolboxProcessLimit int               `json:"toolbox_process_limit,omitempty"`
}

type BackgroundProcessSummary struct {
	ProcessID      string `json:"process_id"`
	State          string `json:"state"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	ExitKnown      bool   `json:"exit_known"`
	ExitCode       int    `json:"exit_code"`
	TerminalSignal string `json:"terminal_signal,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type OperationResult struct {
	WorkspaceID               string                     `json:"workspace_id,omitempty"`
	AuthorizationRevision     uint64                     `json:"authorization_revision,omitempty"`
	JobID                     string                     `json:"job_id,omitempty"`
	JobState                  string                     `json:"job_state,omitempty"`
	ProgressRevision          uint64                     `json:"progress_revision,omitempty"`
	CycleCount                uint64                     `json:"cycle_count,omitempty"`
	JobSafeCode               string                     `json:"job_safe_code,omitempty"`
	Release                   string                     `json:"release,omitempty"`
	Commit                    string                     `json:"commit,omitempty"`
	ManifestStatus            string                     `json:"manifest_status,omitempty"`
	ComponentsCompatible      bool                       `json:"components_compatible,omitempty"`
	ServiceActive             bool                       `json:"service_active,omitempty"`
	ServiceState              string                     `json:"service_state,omitempty"`
	ServiceRestarts           uint64                     `json:"service_restarts,omitempty"`
	ServiceRestartsKnown      bool                       `json:"service_restarts_known,omitempty"`
	ProcessState              string                     `json:"process_state,omitempty"`
	LockState                 string                     `json:"lock_state,omitempty"`
	Coherence                 string                     `json:"coherence,omitempty"`
	ProcessRelease            string                     `json:"process_release,omitempty"`
	ProcessCommit             string                     `json:"process_commit,omitempty"`
	UpdateAvailable           bool                       `json:"update_available"`
	Paired                    bool                       `json:"paired,omitempty"`
	BubblewrapValid           bool                       `json:"bubblewrap_valid,omitempty"`
	RootlessValid             bool                       `json:"rootless_valid,omitempty"`
	WorkspaceCount            int                        `json:"workspace_count,omitempty"`
	ProviderValid             bool                       `json:"provider_valid,omitempty"`
	DriverValid               bool                       `json:"driver_valid,omitempty"`
	Blockers                  []string                   `json:"blockers,omitempty"`
	ProjectAlias              string                     `json:"project_alias,omitempty"`
	ProjectOwner              string                     `json:"project_owner,omitempty"`
	ProjectRepository         string                     `json:"project_repository,omitempty"`
	ProjectTarget             string                     `json:"project_target,omitempty"`
	ProjectState              string                     `json:"project_state,omitempty"`
	ProjectProfile            string                     `json:"project_profile,omitempty"`
	ProjectMode               string                     `json:"project_mode,omitempty"`
	SnapshotBranch            string                     `json:"snapshot_branch,omitempty"`
	SnapshotHead              string                     `json:"snapshot_head,omitempty"`
	SnapshotClean             bool                       `json:"snapshot_clean,omitempty"`
	ExecCompleted             bool                       `json:"exec_completed,omitempty"`
	ExecExitCode              int                        `json:"exec_exit_code,omitempty"`
	ExecStdout                string                     `json:"exec_stdout,omitempty"`
	ExecStderr                string                     `json:"exec_stderr,omitempty"`
	ExecTimedOut              bool                       `json:"exec_timed_out,omitempty"`
	ExecStdoutTruncated       bool                       `json:"exec_stdout_truncated,omitempty"`
	ExecStderrTruncated       bool                       `json:"exec_stderr_truncated,omitempty"`
	ExecTimingKnown           bool                       `json:"exec_timing_known,omitempty"`
	ExecPreflightUS           int64                      `json:"exec_preflight_us,omitempty"`
	ExecExecutionUS           int64                      `json:"exec_execution_us,omitempty"`
	ExecResultUS              int64                      `json:"exec_result_us,omitempty"`
	BackgroundProcessID       string                     `json:"background_process_id,omitempty"`
	BackgroundProcessState    string                     `json:"background_process_state,omitempty"`
	BackgroundStartedAt       string                     `json:"background_started_at,omitempty"`
	BackgroundFinishedAt      string                     `json:"background_finished_at,omitempty"`
	BackgroundExitKnown       bool                       `json:"background_exit_known,omitempty"`
	BackgroundExitCode        int                        `json:"background_exit_code,omitempty"`
	BackgroundTerminalSignal  string                     `json:"background_terminal_signal,omitempty"`
	BackgroundReason          string                     `json:"background_reason,omitempty"`
	BackgroundStdout          string                     `json:"background_stdout,omitempty"`
	BackgroundStderr          string                     `json:"background_stderr,omitempty"`
	BackgroundStdoutNext      int64                      `json:"background_stdout_next,omitempty"`
	BackgroundStderrNext      int64                      `json:"background_stderr_next,omitempty"`
	BackgroundStdoutEOF       bool                       `json:"background_stdout_eof,omitempty"`
	BackgroundStderrEOF       bool                       `json:"background_stderr_eof,omitempty"`
	BackgroundStdoutTruncated bool                       `json:"background_stdout_truncated,omitempty"`
	BackgroundStderrTruncated bool                       `json:"background_stderr_truncated,omitempty"`
	BackgroundProcesses       []BackgroundProcessSummary `json:"background_processes,omitempty"`
	BackgroundCleanupRemoved  int                        `json:"background_cleanup_removed,omitempty"`
	BackgroundCleanupActive   int                        `json:"background_cleanup_active,omitempty"`
	GitBranch                 string                     `json:"git_branch,omitempty"`
	GitHead                   string                     `json:"git_head,omitempty"`
	GitRemoteHead             string                     `json:"git_remote_head,omitempty"`
	GitAhead                  int                        `json:"git_ahead,omitempty"`
	GitBehind                 int                        `json:"git_behind,omitempty"`
	GitDiverged               bool                       `json:"git_diverged,omitempty"`
	GitDetached               bool                       `json:"git_detached,omitempty"`
	GitDirty                  bool                       `json:"git_dirty,omitempty"`
	GitClean                  bool                       `json:"git_clean,omitempty"`
	GitFetched                bool                       `json:"git_fetched,omitempty"`
	GitFastForwarded          bool                       `json:"git_fast_forwarded,omitempty"`
	GitPlanID                 string                     `json:"git_plan_id,omitempty"`
	GitPlanExpiresAt          string                     `json:"git_plan_expires_at,omitempty"`
	GitHubConfigured          bool                       `json:"github_configured,omitempty"`
	GitHubVisibility          string                     `json:"github_visibility,omitempty"`
	GitHubDefaultBranch       string                     `json:"github_default_branch,omitempty"`
	GitHubArchived            bool                       `json:"github_archived,omitempty"`
	GitHubMetadataRead        bool                       `json:"github_metadata_read,omitempty"`
	GitHubContentsRead        bool                       `json:"github_contents_read,omitempty"`
	GitHubContentsWrite       bool                       `json:"github_contents_write,omitempty"`
	GitHubPullRequestsRead    bool                       `json:"github_pull_requests_read,omitempty"`
	GitHubActionsRead         bool                       `json:"github_actions_read,omitempty"`
	GitHubAdministration      bool                       `json:"github_administration,omitempty"`
	GitHubPermissionIssues    []string                   `json:"github_permission_issues,omitempty"`
	ToolboxID                 string                     `json:"toolbox_id,omitempty"`
	ToolboxState              string                     `json:"toolbox_state,omitempty"`
	ToolboxBase               string                     `json:"toolbox_base,omitempty"`
	ToolboxBaseImageID        string                     `json:"toolbox_base_image_id,omitempty"`
	ToolboxCreatedAt          string                     `json:"toolbox_created_at,omitempty"`
	ToolboxUpdatedAt          string                     `json:"toolbox_updated_at,omitempty"`
	ToolboxOutput             string                     `json:"toolbox_output,omitempty"`
	ToolboxOutputTruncated    bool                       `json:"toolbox_output_truncated,omitempty"`
	ToolboxRemoved            bool                       `json:"toolbox_removed,omitempty"`
	ToolboxServiceID          string                     `json:"toolbox_service_id,omitempty"`
	ToolboxServiceName        string                     `json:"toolbox_service_name,omitempty"`
	ToolboxServiceState       string                     `json:"toolbox_service_state,omitempty"`
	ToolboxServiceCreatedAt   string                     `json:"toolbox_service_created_at,omitempty"`
	ToolboxServiceUpdatedAt   string                     `json:"toolbox_service_updated_at,omitempty"`
	ToolboxCPUMillis          int                        `json:"toolbox_cpu_millis,omitempty"`
	ToolboxMemoryMiB          int                        `json:"toolbox_memory_mib,omitempty"`
	ToolboxProcessLimit       int                        `json:"toolbox_process_limit,omitempty"`
	ToolboxContainerAccess    bool                       `json:"toolbox_container_access,omitempty"`
	ToolboxWritableBytes      int64                      `json:"toolbox_writable_bytes,omitempty"`
	ToolboxRootFSBytes        int64                      `json:"toolbox_rootfs_bytes,omitempty"`
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
	LeasedAt        time.Time         `json:"-"`
	RunningAt       time.Time         `json:"-"`
	FinalizingAt    time.Time         `json:"-"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// MarshalJSON preserves compatibility with Edge bundles released before
// operation lifecycle progress was added. encoding/json does not consider a
// zero-valued struct empty for `omitempty`, so without this adapter every lease
// includes an empty `progress` object that strict legacy decoders reject.
func (operation Operation) MarshalJSON() ([]byte, error) {
	type operationAlias Operation
	type operationWire struct {
		operationAlias
		Progress *OperationProgress `json:"progress,omitempty"`
	}
	var progress *OperationProgress
	if operation.Progress != (OperationProgress{}) {
		value := operation.Progress
		progress = &value
	}
	return json.Marshal(operationWire{operationAlias: operationAlias(operation), Progress: progress})
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
	request, err := validateOperationRequestWithProjectExec(kind, request)
	if !idPattern.MatchString(deviceID) || err != nil {
		return Operation{}, false, errors.New("edge operation is invalid")
	}
	body, _ := json.Marshal(request)
	digestInput := body
	if projectOperationUsesIdempotency(kind) {
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
		if projectOperationUsesIdempotency(kind) && !operationRequestsEqual(existing.Request, request) {
			return Operation{}, false, errors.New("edge operation idempotency conflict")
		}
		if kind == OperationLabPrepare || kind == OperationLabRetarget || projectOperationUsesIdempotency(kind) || existing.State == OperationQueued || existing.State == OperationLeased {
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
	if err := recoverExpiredOperationLeases(tx, now, "device_id", deviceID); err != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
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
	if _, err := tx.Exec(`UPDATE edge_operations SET state=?,lease_id=?,lease_until=?,lease_attempts=lease_attempts+1,first_leased_at=COALESCE(first_leased_at,?),leased_at=?,running_at=NULL,finalizing_at=NULL,updated_at=? WHERE operation_id=? AND state=?`, OperationLeased, leaseID, expires.UnixNano(), now.UnixNano(), now.UnixNano(), now.UnixNano(), id, OperationQueued); err != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	op, err := scanOperation(tx.QueryRow(`SELECT operation_id,device_id,kind,request_json,state,result_json,safe_code,created_at,leased_at,running_at,finalizing_at,updated_at FROM edge_operations WHERE operation_id=?`, id))
	if err != nil || tx.Commit() != nil {
		return OperationLease{}, errors.New("operation lease unavailable")
	}
	return OperationLease{Operation: op, LeaseID: leaseID, ExpiresAt: expires}, nil
}

func (s *Store) CompleteOperation(deviceID, operationID, leaseID string, result OperationResult, safeCode string) (Operation, error) {
	if !idPattern.MatchString(deviceID) || !operationIDPattern.MatchString(operationID) || !leaseIDPattern.MatchString(leaseID) {
		return Operation{}, errors.New("operation completion is invalid")
	}
	var kind OperationKind
	if err := s.db.QueryRow(`SELECT kind FROM edge_operations WHERE operation_id=? AND device_id=? AND lease_id=? AND state=?`, operationID, deviceID, leaseID, OperationLeased).Scan(&kind); err != nil {
		return Operation{}, errors.New("active operation lease not found")
	}
	if !validOperationCompletionForKind(kind, result, safeCode) {
		result = OperationResult{}
		safeCode = "operation_result_invalid"
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
	if !projectOperationUsesIdempotency(kind) && request.IdempotencyKey != "" {
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
		if !operationRequestEmpty(request) {
			return OperationRequest{}, errors.New("edge diagnostic request is invalid")
		}
	case OperationBundleUpdate:
		if !operationRequestStableBundle(request) {
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
	if hasProjectExecResult(result) {
		return code == "" && validProjectExecResult(result)
	}
	if hasProjectProcessResult(result) {
		return code == "" && validProjectProcessResult(result)
	}
	if hasProjectGitSyncResult(result) {
		return code == "" && validProjectGitSyncResult(result)
	}
	if hasProjectGitHubResult(result) {
		return code == "" && validProjectGitHubResult(result)
	}
	if hasProjectToolboxServiceResult(result) {
		return code == "" && validProjectToolboxServiceResult(result)
	}
	if hasProjectToolboxResult(result) {
		return code == "" && validProjectToolboxResult(result)
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

func validOperationCompletionForKind(kind OperationKind, result OperationResult, code string) bool {
	if code != "" {
		return validOperationCompletion(result, code)
	}
	if hasProjectExecResult(result) {
		return kind == OperationProjectExec && validOperationCompletion(result, "")
	}
	if kind == OperationProjectProcessList {
		return validProjectProcessListResult(result)
	}
	if kind == OperationProjectProcessCleanup {
		return validProjectProcessCleanupResult(result)
	}
	if hasProjectProcessResult(result) {
		return (kind == OperationProjectProcessStart || kind == OperationProjectProcessStatus || kind == OperationProjectProcessStop || kind == OperationProjectProcessSignal) && validOperationCompletion(result, "")
	}
	if hasProjectGitSyncResult(result) {
		return validProjectGitSyncResultForKind(kind, result)
	}
	if hasProjectGitHubResult(result) {
		return kind == OperationProjectGitHubStatus && validProjectGitHubResult(result)
	}
	if hasProjectToolboxServiceResult(result) {
		return validProjectToolboxServiceResultForKind(kind, result)
	}
	if hasProjectToolboxResult(result) {
		return validProjectToolboxResultForKind(kind, result)
	}
	if result.SnapshotBranch != "" || result.SnapshotHead != "" || result.SnapshotClean {
		return kind == OperationProjectSnapshot && validOperationCompletion(result, "")
	}
	if kind == OperationProjectExec || kind == OperationProjectProcessStart || kind == OperationProjectProcessStatus || kind == OperationProjectProcessStop || kind == OperationProjectProcessSignal || kind == OperationProjectProcessList || kind == OperationProjectProcessCleanup || kind == OperationProjectSnapshot || kind == OperationProjectGitStatus || kind == OperationProjectGitFetch || kind == OperationProjectGitFastForwardPreview || kind == OperationProjectGitFastForward || kind == OperationProjectGitHubStatus || kind == OperationProjectToolboxCreate || kind == OperationProjectToolboxStatus || kind == OperationProjectToolboxExec || kind == OperationProjectToolboxInstall || kind == OperationProjectToolboxCleanup || kind == OperationProjectToolboxRepair || kind == OperationProjectToolboxServiceStart || kind == OperationProjectToolboxServiceStatus || kind == OperationProjectToolboxServiceStop {
		return false
	}
	return validOperationCompletion(result, "")
}

func validProjectOperationResult(result OperationResult) bool {
	if !workspaceIDPattern.MatchString(result.WorkspaceID) ||
		!projectOperationAliasPattern.MatchString(result.ProjectAlias) || !githubOwnerOperationPattern.MatchString(result.ProjectOwner) ||
		!projectOperationRepositoryPattern.MatchString(result.ProjectRepository) || strings.ContainsAny(result.ProjectRepository, `/\\`) ||
		!projectOperationTargetPattern.MatchString(result.ProjectTarget) || (result.ProjectState != "ready" && result.ProjectState != "dirty") ||
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
	if result.SnapshotClean != (result.ProjectState == "ready") || !projectSnapshotBranchPattern.MatchString(branch) ||
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
	legacy := result.ServiceState == "" && result.ServiceRestarts == 0 && !result.ServiceRestartsKnown && result.ProcessState == "" && result.LockState == "" && result.Coherence == "" && result.ProcessRelease == "" && result.ProcessCommit == ""
	if legacy {
		return true
	}
	if !regexp.MustCompile(`^(active|inactive|activating|deactivating|failed|reloading|maintenance)$`).MatchString(result.ServiceState) ||
		!regexp.MustCompile(`^(inactive|single|duplicate|incoherent)$`).MatchString(result.ProcessState) ||
		!regexp.MustCompile(`^(missing|held|stale_recoverable|held_unverified|held_incoherent|unlocked_invalid|unlocked_live_owner|blocked)$`).MatchString(result.LockState) ||
		!regexp.MustCompile(`^(stopped|managed|manual|duplicate|incoherent)$`).MatchString(result.Coherence) {
		return false
	}
	if result.ServiceActive != (result.ServiceState == "active") || (!result.ServiceRestartsKnown && result.ServiceRestarts != 0) {
		return false
	}
	return validRuntimeDiagnosticState(result)
}

func emptyOperationResult(result OperationResult) bool {
	if hasProjectExecResult(result) || hasProjectProcessResult(result) {
		return false
	}
	return result.WorkspaceID == "" && result.AuthorizationRevision == 0 && result.JobID == "" && result.JobState == "" && result.ProgressRevision == 0 && result.CycleCount == 0 && result.JobSafeCode == "" && result.Release == "" && result.Commit == "" && result.ManifestStatus == "" && !result.ComponentsCompatible && !result.ServiceActive && result.ServiceState == "" && result.ServiceRestarts == 0 && !result.ServiceRestartsKnown && result.ProcessState == "" && result.LockState == "" && result.Coherence == "" && result.ProcessRelease == "" && result.ProcessCommit == "" && !result.UpdateAvailable && !result.Paired && !result.BubblewrapValid && !result.RootlessValid && result.WorkspaceCount == 0 && !result.ProviderValid && !result.DriverValid && len(result.Blockers) == 0 && result.ProjectAlias == "" && result.ProjectOwner == "" && result.ProjectRepository == "" && result.ProjectTarget == "" && result.ProjectState == "" && result.ProjectProfile == "" && result.ProjectMode == "" && !hasProjectGitHubResult(result)
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

func nullableOperationTime(value sql.NullInt64) time.Time {
	if !value.Valid || value.Int64 <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value.Int64).UTC()
}

func scanOperation(row rowScanner) (Operation, error) {
	var op Operation
	var request, result []byte
	var created, updated int64
	var leased, running, finalizing sql.NullInt64
	if err := row.Scan(&op.ID, &op.DeviceID, &op.Kind, &request, &op.State, &result, &op.SafeCode, &created, &leased, &running, &finalizing, &updated); err != nil {
		return Operation{}, err
	}
	if json.Unmarshal(request, &op.Request) != nil || (len(result) > 0 && json.Unmarshal(result, &op.Result) != nil) {
		return Operation{}, errors.New("stored operation is invalid")
	}
	op.CreatedAt = time.Unix(0, created).UTC()
	op.LeasedAt = nullableOperationTime(leased)
	op.RunningAt = nullableOperationTime(running)
	op.FinalizingAt = nullableOperationTime(finalizing)
	op.UpdatedAt = time.Unix(0, updated).UTC()
	return op, nil
}
