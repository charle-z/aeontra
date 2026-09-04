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

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
)

type OperationKind string
type OperationState string

// OperationCompatibilityError reports that queued work requires a newer or
// otherwise different Edge bundle. Control-plane recovery operations remain
// leaseable so the device can inspect, update, roll back, or repair itself.
type OperationCompatibilityError struct {
	ExpectedProtocol string `json:"expected_protocol"`
	ExpectedCatalog  string `json:"expected_catalog"`
	ObservedProtocol string `json:"observed_protocol,omitempty"`
	ObservedCatalog  string `json:"observed_catalog,omitempty"`
}

func (e *OperationCompatibilityError) Error() string { return "edge version skew" }

const (
	MaxOperationProgressUnits = 1_000_000_000
	MaxOperationResultBytes   = 64 << 10
)

const (
	OperationLabPrepare                        OperationKind = "lab_prepare"
	OperationLabRetarget                       OperationKind = "lab_retarget"
	OperationAutopilotStart                    OperationKind = "autopilot_start"
	OperationAutopilotPause                    OperationKind = "autopilot_pause"
	OperationAutopilotResume                   OperationKind = "autopilot_resume"
	OperationAutopilotCancel                   OperationKind = "autopilot_cancel"
	OperationBundleStatus                      OperationKind = "bundle_status"
	OperationBundleUpdate                      OperationKind = "bundle_update"
	OperationBundleRollback                    OperationKind = "bundle_rollback"
	OperationEdgeRepair                        OperationKind = "edge_repair"
	OperationOnboardingStatus                  OperationKind = "onboarding_status"
	OperationProjectPrepare                    OperationKind = "project_prepare"
	OperationProjectStatus                     OperationKind = "project_status"
	OperationProjectRegistryList               OperationKind = "project_registry_list"
	OperationProjectReconcile                  OperationKind = "project_reconcile"
	OperationProjectRelease                    OperationKind = "project_release"
	OperationProjectSnapshot                   OperationKind = "project_snapshot"
	OperationProjectExec                       OperationKind = "project_exec"
	OperationProjectNetworkRoute               OperationKind = "project_network_route"
	OperationProjectNetworkProbe               OperationKind = "project_network_probe"
	OperationProjectProcessStart               OperationKind = "project_process_start"
	OperationProjectProcessStatus              OperationKind = "project_process_status"
	OperationProjectProcessStdin               OperationKind = "project_process_stdin"
	OperationProjectProcessStop                OperationKind = "project_process_stop"
	OperationProjectProcessSignal              OperationKind = "project_process_signal"
	OperationProjectProcessList                OperationKind = "project_process_list"
	OperationProjectProcessCleanup             OperationKind = "project_process_cleanup"
	OperationProjectGitStatus                  OperationKind = "project_git_status"
	OperationProjectGitFetch                   OperationKind = "project_git_fetch"
	OperationProjectGitFastForwardPreview      OperationKind = "project_git_fast_forward_preview"
	OperationProjectGitFastForward             OperationKind = "project_git_fast_forward"
	OperationProjectGitPublishPreview          OperationKind = "project_git_publish_preview"
	OperationProjectGitPublish                 OperationKind = "project_git_publish"
	OperationProjectGitHubStatus               OperationKind = "project_github_status"
	OperationProjectToolboxCreate              OperationKind = "project_toolbox_create"
	OperationProjectToolboxStatus              OperationKind = "project_toolbox_status"
	OperationProjectToolboxExec                OperationKind = "project_toolbox_exec"
	OperationProjectToolboxInstall             OperationKind = "project_toolbox_install"
	OperationProjectToolboxCleanup             OperationKind = "project_toolbox_cleanup"
	OperationProjectToolboxRepair              OperationKind = "project_toolbox_repair"
	OperationProjectToolboxServiceStart        OperationKind = "project_toolbox_service_start"
	OperationProjectToolboxServiceStatus       OperationKind = "project_toolbox_service_status"
	OperationProjectToolboxServiceStop         OperationKind = "project_toolbox_service_stop"
	OperationProjectBrowserCreate              OperationKind = "project_browser_create"
	OperationProjectBrowserStatus              OperationKind = "project_browser_status"
	OperationProjectBrowserList                OperationKind = "project_browser_list"
	OperationProjectBrowserRun                 OperationKind = "project_browser_run"
	OperationProjectBrowserArtifactRead        OperationKind = "project_browser_artifact_read"
	OperationProjectBrowserClose               OperationKind = "project_browser_close"
	OperationProjectBrowserCleanup             OperationKind = "project_browser_cleanup"
	OperationProjectBrowserHarnessStart        OperationKind = "project_browser_harness_start"
	OperationProjectBrowserHarnessStatus       OperationKind = "project_browser_harness_status"
	OperationProjectBrowserHarnessList         OperationKind = "project_browser_harness_list"
	OperationProjectBrowserHarnessStop         OperationKind = "project_browser_harness_stop"
	OperationProjectBrowserHarnessCleanup      OperationKind = "project_browser_harness_cleanup"
	OperationProjectBrowserHarnessArtifactList OperationKind = "project_browser_harness_artifact_list"
	OperationProjectBrowserHarnessArtifactRead OperationKind = "project_browser_harness_artifact_read"
	OperationProjectWorktreeCreate             OperationKind = "project_worktree_create"
	OperationProjectWorktreeClaim              OperationKind = "project_worktree_claim"
	OperationProjectWorktreeStatus             OperationKind = "project_worktree_status"
	OperationProjectWorktreeList               OperationKind = "project_worktree_list"
	OperationProjectWorktreeCleanup            OperationKind = "project_worktree_cleanup"

	OperationQueued    OperationState = "queued"
	OperationLeased    OperationState = "leased"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
	OperationCancelled OperationState = "cancelled"
)

var operationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
var operationCatalogPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var projectOperationAliasPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var projectOperationTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
var projectOperationRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
var projectOperationIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var operationProgressPhasePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

type OperationRequest struct {
	Platform                     string            `json:"platform,omitempty"`
	Machine                      string            `json:"machine,omitempty"`
	Target                       string            `json:"target"`
	Difficulty                   string            `json:"difficulty,omitempty"`
	OperatingSystem              string            `json:"operating_system,omitempty"`
	WorkspaceID                  string            `json:"workspace_id,omitempty"`
	RunUntil                     string            `json:"run_until,omitempty"`
	Release                      string            `json:"release,omitempty"`
	Alias                        string            `json:"alias,omitempty"`
	Repository                   string            `json:"repository,omitempty"`
	TargetAlias                  string            `json:"target_alias,omitempty"`
	ProjectClaimGeneration       uint64            `json:"project_claim_generation,omitempty"`
	Profile                      string            `json:"profile,omitempty"`
	IdempotencyKey               string            `json:"idempotency_key,omitempty"`
	Argv                         []string          `json:"argv,omitempty"`
	CWD                          string            `json:"cwd,omitempty"`
	Stdin                        string            `json:"stdin,omitempty"`
	Environment                  map[string]string `json:"environment,omitempty"`
	TimeoutSeconds               int               `json:"timeout_seconds,omitempty"`
	NetworkDestination           string            `json:"network_destination,omitempty"`
	NetworkPorts                 []int             `json:"network_ports,omitempty"`
	NetworkTimeoutMillis         int               `json:"network_timeout_millis,omitempty"`
	BackgroundProcessID          string            `json:"background_process_id,omitempty"`
	ProcessStdinOffset           int64             `json:"process_stdin_offset,omitempty"`
	ProcessStdinClose            bool              `json:"process_stdin_close,omitempty"`
	StdoutOffset                 int64             `json:"stdout_offset,omitempty"`
	StderrOffset                 int64             `json:"stderr_offset,omitempty"`
	OutputLimit                  int               `json:"output_limit,omitempty"`
	GraceSeconds                 int               `json:"grace_seconds,omitempty"`
	BackgroundSignal             string            `json:"background_signal,omitempty"`
	ProcessLimit                 int               `json:"process_limit,omitempty"`
	GitPlanID                    string            `json:"git_plan_id,omitempty"`
	ToolboxServiceID             string            `json:"toolbox_service_id,omitempty"`
	ToolboxServiceName           string            `json:"toolbox_service_name,omitempty"`
	ToolboxCPUMillis             int               `json:"toolbox_cpu_millis,omitempty"`
	ToolboxMemoryMiB             int               `json:"toolbox_memory_mib,omitempty"`
	ToolboxProcessLimit          int               `json:"toolbox_process_limit,omitempty"`
	BrowserSessionID             string            `json:"browser_session_id,omitempty"`
	BrowserNetworkScope          string            `json:"browser_network_scope,omitempty"`
	BrowserInitialURL            string            `json:"browser_initial_url,omitempty"`
	BrowserViewportWidth         int               `json:"browser_viewport_width,omitempty"`
	BrowserViewportHeight        int               `json:"browser_viewport_height,omitempty"`
	BrowserIgnoreHTTPSErrors     bool              `json:"browser_ignore_https_errors,omitempty"`
	BrowserSteps                 []BrowserStep     `json:"browser_steps,omitempty"`
	BrowserCapture               string            `json:"browser_capture,omitempty"`
	BrowserFullPage              bool              `json:"browser_full_page,omitempty"`
	BrowserTimeoutSeconds        int               `json:"browser_timeout_seconds,omitempty"`
	BrowserArtifactID            string            `json:"browser_artifact_id,omitempty"`
	BrowserArtifactOffset        int64             `json:"browser_artifact_offset,omitempty"`
	BrowserArtifactLimit         int               `json:"browser_artifact_limit,omitempty"`
	BrowserHarnessRunID          string            `json:"browser_harness_run_id,omitempty"`
	BrowserHarnessProfile        string            `json:"browser_harness_profile,omitempty"`
	BrowserHarnessTimeoutSeconds int               `json:"browser_harness_timeout_seconds,omitempty"`
	BrowserHarnessStorageMiB     int               `json:"browser_harness_storage_mib,omitempty"`
	BrowserHarnessListLimit      int               `json:"browser_harness_list_limit,omitempty"`
	BrowserHarnessRemoveProfile  bool              `json:"browser_harness_remove_profile,omitempty"`
	BrowserHarnessArtifactPath   string            `json:"browser_harness_artifact_path,omitempty"`
	BrowserHarnessArtifactOffset int64             `json:"browser_harness_artifact_offset,omitempty"`
	BrowserHarnessArtifactLimit  int               `json:"browser_harness_artifact_limit,omitempty"`
	WorktreeID                   string            `json:"worktree_id,omitempty"`
	WorktreeBaseCommit           string            `json:"worktree_base_commit,omitempty"`
	WorktreeRole                 string            `json:"worktree_role,omitempty"`
	WorkJobID                    string            `json:"work_job_id,omitempty"`
	WorkLeaseID                  string            `json:"work_lease_id,omitempty"`
	WorkFence                    uint64            `json:"work_fence,omitempty"`
	WorktreeLimit                int               `json:"worktree_limit,omitempty"`
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

type NetworkPortResult struct {
	Port  int    `json:"port"`
	State string `json:"state"`
}

type BrowserHarnessSummary struct {
	RunID          string `json:"run_id"`
	State          string `json:"state"`
	Profile        string `json:"profile"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	StartedAt      string `json:"started_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
	ExitKnown      bool   `json:"exit_known"`
	ExitCode       int    `json:"exit_code"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	StorageMiB     int    `json:"storage_mib"`
}

type BrowserHarnessArtifactSummary struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	UpdatedAt string `json:"updated_at"`
}

type ProjectWorktreeSummary struct {
	WorktreeID  string `json:"worktree_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	State       string `json:"state"`
	Role        string `json:"role"`
	BaseCommit  string `json:"base_commit"`
	Branch      string `json:"branch"`
	JobID       string `json:"job_id"`
	LeaseID     string `json:"lease_id"`
	Fence       uint64 `json:"fence"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ProjectClaimSummary is the safe, path-free representation of one durable
// project registry claim returned by recovery operations.
type ProjectClaimSummary struct {
	Alias       string `json:"alias"`
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
	Target      string `json:"target"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Generation  uint64 `json:"generation"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Repairable  bool   `json:"repairable"`
}

type OperationResult struct {
	WorkspaceID           string `json:"workspace_id,omitempty"`
	AuthorizationRevision uint64 `json:"authorization_revision,omitempty"`
	JobID                 string `json:"job_id,omitempty"`
	JobState              string `json:"job_state,omitempty"`
	ProgressRevision      uint64 `json:"progress_revision,omitempty"`
	CycleCount            uint64 `json:"cycle_count,omitempty"`
	JobSafeCode           string `json:"job_safe_code,omitempty"`
	Release               string `json:"release,omitempty"`
	Commit                string `json:"commit,omitempty"`
	// These fields identify the authenticated Edge bundle independently from
	// the MCP backend identity, so protocol/catalog skew is observable without
	// exposing paths or other private state.
	EdgeProtocolVersion              string                          `json:"edge_protocol_version,omitempty"`
	EdgeCatalogHash                  string                          `json:"edge_catalog_hash,omitempty"`
	ManifestStatus                   string                          `json:"manifest_status,omitempty"`
	ComponentsCompatible             bool                            `json:"components_compatible,omitempty"`
	ServiceActive                    bool                            `json:"service_active,omitempty"`
	ServiceState                     string                          `json:"service_state,omitempty"`
	ServiceRestarts                  uint64                          `json:"service_restarts,omitempty"`
	ServiceRestartsKnown             bool                            `json:"service_restarts_known,omitempty"`
	ProcessState                     string                          `json:"process_state,omitempty"`
	LockState                        string                          `json:"lock_state,omitempty"`
	Coherence                        string                          `json:"coherence,omitempty"`
	ProcessRelease                   string                          `json:"process_release,omitempty"`
	ProcessCommit                    string                          `json:"process_commit,omitempty"`
	UpdateAvailable                  bool                            `json:"update_available"`
	Paired                           bool                            `json:"paired,omitempty"`
	BubblewrapValid                  bool                            `json:"bubblewrap_valid,omitempty"`
	RootlessValid                    bool                            `json:"rootless_valid,omitempty"`
	WorkspaceCount                   int                             `json:"workspace_count,omitempty"`
	ProviderValid                    bool                            `json:"provider_valid,omitempty"`
	DriverValid                      bool                            `json:"driver_valid,omitempty"`
	Blockers                         []string                        `json:"blockers,omitempty"`
	ProjectAlias                     string                          `json:"project_alias,omitempty"`
	ProjectOwner                     string                          `json:"project_owner,omitempty"`
	ProjectRepository                string                          `json:"project_repository,omitempty"`
	ProjectTarget                    string                          `json:"project_target,omitempty"`
	ProjectState                     string                          `json:"project_state,omitempty"`
	ProjectProfile                   string                          `json:"project_profile,omitempty"`
	ProjectMode                      string                          `json:"project_mode,omitempty"`
	ProjectReason                    string                          `json:"project_reason,omitempty"`
	ProjectDiagnosticReason          string                          `json:"project_diagnostic_reason,omitempty"`
	ProjectRepairable                bool                            `json:"project_repairable,omitempty"`
	ProjectRecommendedAction         string                          `json:"project_recommended_action,omitempty"`
	ProjectRegistryAction            string                          `json:"project_registry_action,omitempty"`
	ProjectClaimGeneration           uint64                          `json:"project_claim_generation,omitempty"`
	ProjectClaims                    []ProjectClaimSummary           `json:"project_claims,omitempty"`
	ProjectToolchainState            string                          `json:"project_toolchain_state,omitempty"`
	ProjectToolchainRoute            string                          `json:"project_toolchain_route,omitempty"`
	ProjectToolchainManifests        []string                        `json:"project_toolchain_manifests,omitempty"`
	SnapshotBranch                   string                          `json:"snapshot_branch,omitempty"`
	SnapshotHead                     string                          `json:"snapshot_head,omitempty"`
	SnapshotClean                    bool                            `json:"snapshot_clean,omitempty"`
	ExecCompleted                    bool                            `json:"exec_completed,omitempty"`
	ExecExitCode                     int                             `json:"exec_exit_code,omitempty"`
	ExecStdout                       string                          `json:"exec_stdout,omitempty"`
	ExecStderr                       string                          `json:"exec_stderr,omitempty"`
	ExecTimedOut                     bool                            `json:"exec_timed_out,omitempty"`
	ExecStdoutTruncated              bool                            `json:"exec_stdout_truncated,omitempty"`
	ExecStderrTruncated              bool                            `json:"exec_stderr_truncated,omitempty"`
	ExecTimingKnown                  bool                            `json:"exec_timing_known,omitempty"`
	ExecPreflightUS                  int64                           `json:"exec_preflight_us,omitempty"`
	ExecExecutionUS                  int64                           `json:"exec_execution_us,omitempty"`
	ExecResultUS                     int64                           `json:"exec_result_us,omitempty"`
	NetworkDestination               string                          `json:"network_destination,omitempty"`
	NetworkInterface                 string                          `json:"network_interface,omitempty"`
	NetworkSourceIP                  string                          `json:"network_source_ip,omitempty"`
	NetworkPorts                     []NetworkPortResult             `json:"network_ports,omitempty"`
	BackgroundProcessID              string                          `json:"background_process_id,omitempty"`
	BackgroundProcessState           string                          `json:"background_process_state,omitempty"`
	BackgroundStartedAt              string                          `json:"background_started_at,omitempty"`
	BackgroundFinishedAt             string                          `json:"background_finished_at,omitempty"`
	BackgroundExitKnown              bool                            `json:"background_exit_known,omitempty"`
	BackgroundExitCode               int                             `json:"background_exit_code,omitempty"`
	BackgroundTerminalSignal         string                          `json:"background_terminal_signal,omitempty"`
	BackgroundReason                 string                          `json:"background_reason,omitempty"`
	BackgroundStdout                 string                          `json:"background_stdout,omitempty"`
	BackgroundStderr                 string                          `json:"background_stderr,omitempty"`
	BackgroundStdoutNext             int64                           `json:"background_stdout_next,omitempty"`
	BackgroundStderrNext             int64                           `json:"background_stderr_next,omitempty"`
	BackgroundStdoutEOF              bool                            `json:"background_stdout_eof,omitempty"`
	BackgroundStderrEOF              bool                            `json:"background_stderr_eof,omitempty"`
	BackgroundStdoutTruncated        bool                            `json:"background_stdout_truncated,omitempty"`
	BackgroundStderrTruncated        bool                            `json:"background_stderr_truncated,omitempty"`
	BackgroundStdinNext              int64                           `json:"background_stdin_next,omitempty"`
	BackgroundStdinAccepted          int                             `json:"background_stdin_accepted,omitempty"`
	BackgroundStdinClosed            bool                            `json:"background_stdin_closed,omitempty"`
	BackgroundStdinReused            bool                            `json:"background_stdin_reused,omitempty"`
	BackgroundProcesses              []BackgroundProcessSummary      `json:"background_processes,omitempty"`
	BackgroundCleanupRemoved         int                             `json:"background_cleanup_removed,omitempty"`
	BackgroundCleanupActive          int                             `json:"background_cleanup_active,omitempty"`
	GitBranch                        string                          `json:"git_branch,omitempty"`
	GitHead                          string                          `json:"git_head,omitempty"`
	GitRemoteHead                    string                          `json:"git_remote_head,omitempty"`
	GitAhead                         int                             `json:"git_ahead,omitempty"`
	GitBehind                        int                             `json:"git_behind,omitempty"`
	GitDiverged                      bool                            `json:"git_diverged,omitempty"`
	GitDetached                      bool                            `json:"git_detached,omitempty"`
	GitDirty                         bool                            `json:"git_dirty,omitempty"`
	GitClean                         bool                            `json:"git_clean,omitempty"`
	GitFetched                       bool                            `json:"git_fetched,omitempty"`
	GitFastForwarded                 bool                            `json:"git_fast_forwarded,omitempty"`
	GitPublished                     bool                            `json:"git_published,omitempty"`
	GitPlanID                        string                          `json:"git_plan_id,omitempty"`
	GitPlanExpiresAt                 string                          `json:"git_plan_expires_at,omitempty"`
	GitHubConfigured                 bool                            `json:"github_configured,omitempty"`
	GitHubVisibility                 string                          `json:"github_visibility,omitempty"`
	GitHubDefaultBranch              string                          `json:"github_default_branch,omitempty"`
	GitHubArchived                   bool                            `json:"github_archived,omitempty"`
	GitHubMetadataRead               bool                            `json:"github_metadata_read,omitempty"`
	GitHubContentsRead               bool                            `json:"github_contents_read,omitempty"`
	GitHubContentsWrite              bool                            `json:"github_contents_write,omitempty"`
	GitHubPullRequestsRead           bool                            `json:"github_pull_requests_read,omitempty"`
	GitHubActionsRead                bool                            `json:"github_actions_read,omitempty"`
	GitHubAdministration             bool                            `json:"github_administration,omitempty"`
	GitHubPermissionIssues           []string                        `json:"github_permission_issues,omitempty"`
	ToolboxID                        string                          `json:"toolbox_id,omitempty"`
	ToolboxState                     string                          `json:"toolbox_state,omitempty"`
	ToolboxBase                      string                          `json:"toolbox_base,omitempty"`
	ToolboxBaseImageID               string                          `json:"toolbox_base_image_id,omitempty"`
	ToolboxCreatedAt                 string                          `json:"toolbox_created_at,omitempty"`
	ToolboxUpdatedAt                 string                          `json:"toolbox_updated_at,omitempty"`
	ToolboxOutput                    string                          `json:"toolbox_output,omitempty"`
	ToolboxOutputTruncated           bool                            `json:"toolbox_output_truncated,omitempty"`
	ToolboxRemoved                   bool                            `json:"toolbox_removed,omitempty"`
	ToolboxServiceID                 string                          `json:"toolbox_service_id,omitempty"`
	ToolboxServiceName               string                          `json:"toolbox_service_name,omitempty"`
	ToolboxServiceState              string                          `json:"toolbox_service_state,omitempty"`
	ToolboxServiceCreatedAt          string                          `json:"toolbox_service_created_at,omitempty"`
	ToolboxServiceUpdatedAt          string                          `json:"toolbox_service_updated_at,omitempty"`
	ToolboxCPUMillis                 int                             `json:"toolbox_cpu_millis,omitempty"`
	ToolboxMemoryMiB                 int                             `json:"toolbox_memory_mib,omitempty"`
	ToolboxProcessLimit              int                             `json:"toolbox_process_limit,omitempty"`
	ToolboxContainerAccess           bool                            `json:"toolbox_container_access,omitempty"`
	ToolboxWritableBytes             int64                           `json:"toolbox_writable_bytes,omitempty"`
	ToolboxRootFSBytes               int64                           `json:"toolbox_rootfs_bytes,omitempty"`
	BrowserSessionID                 string                          `json:"browser_session_id,omitempty"`
	BrowserState                     string                          `json:"browser_state,omitempty"`
	BrowserNetworkScope              string                          `json:"browser_network_scope,omitempty"`
	BrowserSafeURL                   string                          `json:"browser_safe_url,omitempty"`
	BrowserTitle                     string                          `json:"browser_title,omitempty"`
	BrowserRevision                  uint64                          `json:"browser_revision,omitempty"`
	BrowserCreatedAt                 string                          `json:"browser_created_at,omitempty"`
	BrowserUpdatedAt                 string                          `json:"browser_updated_at,omitempty"`
	BrowserText                      string                          `json:"browser_text,omitempty"`
	BrowserTextTruncated             bool                            `json:"browser_text_truncated,omitempty"`
	BrowserArtifactID                string                          `json:"browser_artifact_id,omitempty"`
	BrowserArtifactMediaType         string                          `json:"browser_artifact_media_type,omitempty"`
	BrowserArtifactBytes             int64                           `json:"browser_artifact_bytes,omitempty"`
	BrowserArtifactSHA256            string                          `json:"browser_artifact_sha256,omitempty"`
	BrowserArtifactOffset            int64                           `json:"browser_artifact_offset,omitempty"`
	BrowserArtifactNext              int64                           `json:"browser_artifact_next,omitempty"`
	BrowserArtifactEOF               bool                            `json:"browser_artifact_eof,omitempty"`
	BrowserArtifactDataBase64        string                          `json:"browser_artifact_data_base64,omitempty"`
	BrowserSessions                  []BrowserSessionSummary         `json:"browser_sessions,omitempty"`
	BrowserListComplete              bool                            `json:"browser_list_complete,omitempty"`
	BrowserCleanupCompleted          bool                            `json:"browser_cleanup_completed,omitempty"`
	BrowserCleanupRemoved            int                             `json:"browser_cleanup_removed,omitempty"`
	BrowserCleanupArtifacts          int                             `json:"browser_cleanup_artifacts,omitempty"`
	BrowserHarnessRunID              string                          `json:"browser_harness_run_id,omitempty"`
	BrowserHarnessState              string                          `json:"browser_harness_state,omitempty"`
	BrowserHarnessProfile            string                          `json:"browser_harness_profile,omitempty"`
	BrowserHarnessCreatedAt          string                          `json:"browser_harness_created_at,omitempty"`
	BrowserHarnessUpdatedAt          string                          `json:"browser_harness_updated_at,omitempty"`
	BrowserHarnessStartedAt          string                          `json:"browser_harness_started_at,omitempty"`
	BrowserHarnessFinishedAt         string                          `json:"browser_harness_finished_at,omitempty"`
	BrowserHarnessExitKnown          bool                            `json:"browser_harness_exit_known,omitempty"`
	BrowserHarnessExitCode           int                             `json:"browser_harness_exit_code,omitempty"`
	BrowserHarnessTimeoutSeconds     int                             `json:"browser_harness_timeout_seconds,omitempty"`
	BrowserHarnessStorageMiB         int                             `json:"browser_harness_storage_mib,omitempty"`
	BrowserHarnessStdout             string                          `json:"browser_harness_stdout,omitempty"`
	BrowserHarnessStderr             string                          `json:"browser_harness_stderr,omitempty"`
	BrowserHarnessStdoutNext         int64                           `json:"browser_harness_stdout_next,omitempty"`
	BrowserHarnessStderrNext         int64                           `json:"browser_harness_stderr_next,omitempty"`
	BrowserHarnessStdoutEOF          bool                            `json:"browser_harness_stdout_eof,omitempty"`
	BrowserHarnessStderrEOF          bool                            `json:"browser_harness_stderr_eof,omitempty"`
	BrowserHarnessStdoutTruncated    bool                            `json:"browser_harness_stdout_truncated,omitempty"`
	BrowserHarnessStderrTruncated    bool                            `json:"browser_harness_stderr_truncated,omitempty"`
	BrowserHarnessArtifactCount      int                             `json:"browser_harness_artifact_count,omitempty"`
	BrowserHarnessArtifactBytes      int64                           `json:"browser_harness_artifact_bytes,omitempty"`
	BrowserHarnessRuns               []BrowserHarnessSummary         `json:"browser_harness_runs,omitempty"`
	BrowserHarnessListComplete       bool                            `json:"browser_harness_list_complete,omitempty"`
	BrowserHarnessArtifacts          []BrowserHarnessArtifactSummary `json:"browser_harness_artifacts,omitempty"`
	BrowserHarnessArtifactsComplete  bool                            `json:"browser_harness_artifacts_complete,omitempty"`
	BrowserHarnessArtifactPath       string                          `json:"browser_harness_artifact_path,omitempty"`
	BrowserHarnessArtifactMediaType  string                          `json:"browser_harness_artifact_media_type,omitempty"`
	BrowserHarnessArtifactSHA256     string                          `json:"browser_harness_artifact_sha256,omitempty"`
	BrowserHarnessArtifactOffset     int64                           `json:"browser_harness_artifact_offset,omitempty"`
	BrowserHarnessArtifactNext       int64                           `json:"browser_harness_artifact_next,omitempty"`
	BrowserHarnessArtifactEOF        bool                            `json:"browser_harness_artifact_eof,omitempty"`
	BrowserHarnessArtifactDataBase64 string                          `json:"browser_harness_artifact_data_base64,omitempty"`
	BrowserHarnessCleanupComplete    bool                            `json:"browser_harness_cleanup_complete,omitempty"`
	BrowserHarnessCleanupRuns        int                             `json:"browser_harness_cleanup_runs,omitempty"`
	BrowserHarnessCleanupArtifacts   int                             `json:"browser_harness_cleanup_artifacts,omitempty"`
	BrowserHarnessCleanupProfiles    int                             `json:"browser_harness_cleanup_profiles,omitempty"`
	WorktreeID                       string                          `json:"worktree_id,omitempty"`
	WorktreeState                    string                          `json:"worktree_state,omitempty"`
	WorktreeRole                     string                          `json:"worktree_role,omitempty"`
	WorktreeBaseCommit               string                          `json:"worktree_base_commit,omitempty"`
	WorktreeBranch                   string                          `json:"worktree_branch,omitempty"`
	WorktreeEvidenceKnown            bool                            `json:"worktree_evidence_known,omitempty"`
	WorktreeHeadCommit               string                          `json:"worktree_head_commit,omitempty"`
	WorktreeClean                    bool                            `json:"worktree_clean,omitempty"`
	WorktreeCommitsAheadBase         int                             `json:"worktree_commits_ahead_base,omitempty"`
	WorktreeChangedPathCount         int                             `json:"worktree_changed_path_count,omitempty"`
	WorkJobID                        string                          `json:"work_job_id,omitempty"`
	WorkLeaseID                      string                          `json:"work_lease_id,omitempty"`
	WorkFence                        uint64                          `json:"work_fence,omitempty"`
	WorktreeCreatedAt                string                          `json:"worktree_created_at,omitempty"`
	WorktreeUpdatedAt                string                          `json:"worktree_updated_at,omitempty"`
	Worktrees                        []ProjectWorktreeSummary        `json:"worktrees,omitempty"`
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
		return Operation{}, false, errors.New("edge operation persistence failed: recovery")
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
		return Operation{}, false, errors.New("edge operation persistence failed: lookup")
	}
	if err := s.ensureOperationStorageCapacityLocked(); err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed: insert")
	}
	id, err := randomOpaque("eo_", 16)
	if err != nil {
		return Operation{}, false, errors.New("edge operation generation failed")
	}
	now := s.now().UTC()
	if _, err := s.db.Exec(`INSERT INTO edge_operations(operation_id,device_id,kind,request_json,request_digest,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, id, deviceID, kind, body, digest, OperationQueued, now.UnixNano(), now.UnixNano()); err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed: insert")
	}
	return Operation{ID: id, DeviceID: deviceID, Kind: kind, Request: request, State: OperationQueued, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (s *Store) LeaseOperation(deviceID string, ttl time.Duration) (OperationLease, error) {
	return s.LeaseOperationCompatible(deviceID, ttl, "", "")
}

// SetExpectedOperationCompatibility configures the bundle identity required
// for ordinary operations. It is set once by the server after constructing its
// deterministic MCP catalog.
func (s *Store) SetExpectedOperationCompatibility(protocol, catalog string) error {
	protocol = strings.TrimSpace(protocol)
	catalog = strings.TrimSpace(catalog)
	if protocol == "" || !operationCatalogPattern.MatchString(catalog) {
		return errors.New("edge operation compatibility is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expectedOperationProtocol = protocol
	s.expectedOperationCatalog = catalog
	return nil
}

// LeaseOperationCompatible leases only work supported by the authenticated
// Edge bundle. A mismatched device may still lease recovery operations needed
// to converge to the server catalog.
func (s *Store) LeaseOperationCompatible(deviceID string, ttl time.Duration, observedProtocol, observedCatalog string) (OperationLease, error) {
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
	expectedProtocol := s.expectedOperationProtocol
	expectedCatalog := s.expectedOperationCatalog
	compatible := expectedProtocol == "" || (strings.TrimSpace(observedProtocol) == expectedProtocol && strings.TrimSpace(observedCatalog) == expectedCatalog)
	var id string
	var selectErr error
	if compatible {
		selectErr = tx.QueryRow(`SELECT operation_id FROM edge_operations WHERE device_id=? AND state=? ORDER BY updated_at,operation_id LIMIT 1`, deviceID, OperationQueued).Scan(&id)
	} else {
		selectErr = tx.QueryRow(`SELECT operation_id FROM edge_operations WHERE device_id=? AND state=? AND kind IN (?,?,?,?,?) ORDER BY updated_at,operation_id LIMIT 1`, deviceID, OperationQueued, OperationBundleStatus, OperationBundleUpdate, OperationBundleRollback, OperationEdgeRepair, OperationOnboardingStatus).Scan(&id)
	}
	if selectErr != nil {
		if errors.Is(selectErr, sql.ErrNoRows) {
			if !compatible {
				var queued int
				if err := tx.QueryRow(`SELECT COUNT(1) FROM edge_operations WHERE device_id=? AND state=?`, deviceID, OperationQueued).Scan(&queued); err != nil {
					return OperationLease{}, errors.New("operation lease unavailable")
				}
				if queued > 0 {
					return OperationLease{}, &OperationCompatibilityError{ExpectedProtocol: expectedProtocol, ExpectedCatalog: expectedCatalog, ObservedProtocol: strings.TrimSpace(observedProtocol), ObservedCatalog: strings.TrimSpace(observedCatalog)}
				}
			}
			if err := tx.Commit(); err != nil {
				return OperationLease{}, errors.New("operation lease unavailable")
			}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	// Read the operation kind under the same store lock used by cancellation,
	// lease recovery, and the terminal update.  Keeping validation and commit in
	// one critical section prevents a cancelled or recovered lease from changing
	// underneath completion validation.
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

// OperationByIdempotency recovers the durable operation created for an exact
// idempotent intent. It closes the cross-store crash window between operation
// creation and the coordinator persisting the opaque operation id.
func (s *Store) OperationByIdempotency(deviceID string, kind OperationKind, key string) (Operation, bool, error) {
	key = strings.TrimSpace(key)
	if !idPattern.MatchString(deviceID) || !projectOperationUsesIdempotency(kind) || !projectOperationIdempotencyPattern.MatchString(key) {
		return Operation{}, false, errors.New("edge operation idempotency lookup is invalid")
	}
	sum := sha256.Sum256([]byte(key))
	digest := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverExpiredOperationLeasesForDeviceLocked(deviceID); err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed")
	}
	op, err := s.operationLifecycleByDigest(deviceID, kind, digest)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, errors.New("edge operation persistence failed")
	}
	return op, true, nil
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
	case OperationProjectRegistryList:
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		if !projectOperationTargetPattern.MatchString(request.TargetAlias) || request.Profile != "linux-workcell" ||
			request.Alias != "" || request.Repository != "" || request.Platform != "" || request.Machine != "" || request.Target != "" ||
			request.Difficulty != "" || request.OperatingSystem != "" || request.WorkspaceID != "" || request.RunUntil != "" || request.Release != "" ||
			request.ProjectClaimGeneration != 0 {
			return OperationRequest{}, errors.New("project registry list request is invalid")
		}
	case OperationProjectReconcile:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		if !validProjectOperationRequestCommon(request) || request.Repository != "" || request.ProjectClaimGeneration != 0 {
			return OperationRequest{}, errors.New("project reconcile request is invalid")
		}
	case OperationProjectRelease:
		request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
		request.Repository = strings.ToLower(strings.TrimSpace(request.Repository))
		request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
		request.Profile = strings.TrimSpace(request.Profile)
		if !validProjectOperationRequestCommon(request) || !projectOperationRepositoryPattern.MatchString(request.Repository) ||
			strings.ContainsAny(request.Repository, `/\\`) || strings.HasPrefix(request.Repository, ".") || request.ProjectClaimGeneration == 0 || request.ProjectClaimGeneration > uint64(1<<63-1) {
			return OperationRequest{}, errors.New("project release request is invalid")
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
	if !validEdgeBundleIdentity(result) {
		return false
	}
	if hasProjectWorktreeResult(result) {
		return false
	}
	if hasProjectExecResult(result) {
		return code == "" && validProjectExecResult(result)
	}
	if hasProjectNetworkResult(result) {
		return false
	}
	if hasProjectBrowserHarnessResult(result) {
		return false
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
		validBundle := bundle.ValidRelease(result.Release) && projectSnapshotCommitPattern.MatchString(result.Commit) && result.ManifestStatus == "valid" && result.ComponentsCompatible && result.ProviderValid && result.DriverValid
		invalidComponents := !result.ProviderValid && (!result.DriverValid || result.ManifestStatus == "provider_outdated")
		invalidBundle := result.Release == "" && result.Commit == "" && regexp.MustCompile(`^(bundle_mismatch|provider_outdated|driver_outdated|manifest_invalid)$`).MatchString(result.ManifestStatus) && !result.ComponentsCompatible && invalidComponents
		return (validBundle || invalidBundle) && validDiagnosticBlockers(result.Blockers) && validRuntimeDiagnostic(result)
	}
	if result.JobID != "" {
		return regexp.MustCompile(`^aj_[a-f0-9]{32}$`).MatchString(result.JobID) && regexp.MustCompile(`^(running|paused|blocked|completed|cancelled)$`).MatchString(result.JobState) && (result.JobSafeCode == "" || regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(result.JobSafeCode))
	}
	return result.AuthorizationRevision > 0
}

// validEdgeBundleIdentity accepts legacy diagnostics that predate the explicit
// bundle identity fields, but never accepts a partial or fabricated identity.
// A successful diagnostic is only authoritative when it names the bundle
// protocol this binary understands and carries a canonical catalog digest.
func validEdgeBundleIdentity(result OperationResult) bool {
	if result.EdgeProtocolVersion == "" && result.EdgeCatalogHash == "" {
		return true
	}
	return result.EdgeProtocolVersion == buildinfo.EdgeBundleProtocolVersion &&
		regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(result.EdgeCatalogHash)
}

func validOperationCompletionForKind(kind OperationKind, result OperationResult, code string) bool {
	if code != "" {
		return validOperationCompletion(result, code)
	}
	if hasProjectToolchainSummary(result) && kind != OperationProjectStatus {
		return false
	}
	if hasProjectRegistryResult(result) {
		return validProjectRegistryResultForKind(kind, result)
	}
	if hasProjectDiagnosticResult(result) {
		return kind == OperationProjectStatus && validProjectDiagnosticResult(result)
	}
	if hasProjectWorktreeResult(result) {
		return validProjectWorktreeResultForKind(kind, result)
	}
	if hasProjectExecResult(result) {
		return kind == OperationProjectExec && validOperationCompletion(result, "")
	}
	if hasProjectNetworkResult(result) {
		return validProjectNetworkResultForKind(kind, result)
	}
	if hasProjectBrowserHarnessResult(result) {
		return validProjectBrowserHarnessResultForKind(kind, result)
	}
	if hasProjectBrowserResult(result) {
		return validProjectBrowserResultForKind(kind, result)
	}
	if kind == OperationProjectProcessList {
		return validProjectProcessListResult(result)
	}
	if kind == OperationProjectProcessCleanup {
		return validProjectProcessCleanupResult(result)
	}
	if hasProjectProcessResult(result) {
		if kind == OperationProjectProcessStdin {
			return validProjectProcessStdinResult(result)
		}
		if hasProjectProcessStdinReceipt(result) {
			return false
		}
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
	if kind == OperationProjectNetworkRoute || kind == OperationProjectNetworkProbe {
		return false
	}
	if kind == OperationProjectRegistryList || kind == OperationProjectReconcile || kind == OperationProjectRelease || kind == OperationProjectExec || kind == OperationProjectWorktreeCreate || kind == OperationProjectWorktreeClaim || kind == OperationProjectWorktreeStatus || kind == OperationProjectWorktreeList || kind == OperationProjectWorktreeCleanup || kind == OperationProjectBrowserHarnessStart || kind == OperationProjectBrowserHarnessStatus || kind == OperationProjectBrowserHarnessList || kind == OperationProjectBrowserHarnessStop || kind == OperationProjectBrowserHarnessCleanup || kind == OperationProjectBrowserHarnessArtifactList || kind == OperationProjectBrowserHarnessArtifactRead || kind == OperationProjectBrowserCreate || kind == OperationProjectBrowserStatus || kind == OperationProjectBrowserList || kind == OperationProjectBrowserRun || kind == OperationProjectBrowserArtifactRead || kind == OperationProjectBrowserClose || kind == OperationProjectBrowserCleanup || kind == OperationProjectProcessStart || kind == OperationProjectProcessStatus || kind == OperationProjectProcessStdin || kind == OperationProjectProcessStop || kind == OperationProjectProcessSignal || kind == OperationProjectProcessList || kind == OperationProjectProcessCleanup || kind == OperationProjectSnapshot || kind == OperationProjectGitStatus || kind == OperationProjectGitFetch || kind == OperationProjectGitFastForwardPreview || kind == OperationProjectGitFastForward || kind == OperationProjectGitPublishPreview || kind == OperationProjectGitPublish || kind == OperationProjectGitHubStatus || kind == OperationProjectToolboxCreate || kind == OperationProjectToolboxStatus || kind == OperationProjectToolboxExec || kind == OperationProjectToolboxInstall || kind == OperationProjectToolboxCleanup || kind == OperationProjectToolboxRepair || kind == OperationProjectToolboxServiceStart || kind == OperationProjectToolboxServiceStatus || kind == OperationProjectToolboxServiceStop {
		return false
	}
	return validOperationCompletion(result, "")
}

func hasProjectDiagnosticResult(result OperationResult) bool {
	return result.ProjectReason != "" || result.ProjectDiagnosticReason != "" || result.ProjectRepairable || result.ProjectRecommendedAction != "" ||
		(result.ProjectState != "" && result.ProjectState != "ready" && result.ProjectState != "dirty")
}

func hasProjectRegistryResult(result OperationResult) bool {
	return result.ProjectRegistryAction != "" || len(result.ProjectClaims) > 0 || result.ProjectClaimGeneration > 0
}

func validProjectRegistryResultForKind(kind OperationKind, result OperationResult) bool {
	switch kind {
	case OperationProjectRegistryList, OperationProjectReconcile:
		if result.ProjectRegistryAction != map[OperationKind]string{
			OperationProjectRegistryList: "listed",
			OperationProjectReconcile:    "reconciled",
		}[kind] || len(result.ProjectClaims) > 32 || result.ProjectClaimGeneration != 0 {
			return false
		}
		for _, claim := range result.ProjectClaims {
			if !validProjectClaimSummary(claim) {
				return false
			}
		}
		metadata := result
		metadata.ProjectRegistryAction = ""
		metadata.ProjectClaims = nil
		return emptyOperationResult(metadata)
	case OperationProjectRelease:
		if result.ProjectRegistryAction != "released" || result.ProjectClaimGeneration == 0 || result.ProjectClaimGeneration > uint64(1<<63-1) ||
			!projectOperationAliasPattern.MatchString(result.ProjectAlias) || !githubOwnerOperationPattern.MatchString(result.ProjectOwner) ||
			!projectOperationRepositoryPattern.MatchString(result.ProjectRepository) || strings.ContainsAny(result.ProjectRepository, `/\\`) ||
			!projectOperationTargetPattern.MatchString(result.ProjectTarget) || result.ProjectState != "released" ||
			result.WorkspaceID != "" || result.ProjectProfile != "" || result.ProjectMode != "" || len(result.ProjectClaims) != 0 {
			return false
		}
		metadata := result
		metadata.ProjectAlias, metadata.ProjectOwner, metadata.ProjectRepository, metadata.ProjectTarget, metadata.ProjectState = "", "", "", "", ""
		metadata.ProjectRegistryAction = ""
		metadata.ProjectClaimGeneration = 0
		return emptyOperationResult(metadata)
	default:
		return false
	}
}

func validProjectClaimSummary(claim ProjectClaimSummary) bool {
	if !projectOperationAliasPattern.MatchString(claim.Alias) || !githubOwnerOperationPattern.MatchString(claim.Owner) ||
		!projectOperationRepositoryPattern.MatchString(claim.Repository) || strings.ContainsAny(claim.Repository, `/\\`) ||
		!projectOperationTargetPattern.MatchString(claim.Target) || claim.Generation == 0 || claim.Generation > uint64(1<<63-1) ||
		(claim.WorkspaceID != "" && !workspaceIDPattern.MatchString(claim.WorkspaceID)) ||
		(claim.State != "healthy" && claim.State != "stale" && claim.State != "repairable") ||
		(claim.Reason != "" && !regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(claim.Reason)) {
		return false
	}
	if claim.State == "healthy" && (claim.Reason != "" || claim.Repairable) {
		return false
	}
	if claim.State != "healthy" && !claim.Repairable {
		return false
	}
	return true
}

func validProjectOperationResult(result OperationResult) bool {
	if result.ProjectState != "ready" && result.ProjectState != "dirty" {
		return validProjectDiagnosticResult(result)
	}
	if !workspaceIDPattern.MatchString(result.WorkspaceID) ||
		!projectOperationAliasPattern.MatchString(result.ProjectAlias) || !githubOwnerOperationPattern.MatchString(result.ProjectOwner) ||
		!projectOperationRepositoryPattern.MatchString(result.ProjectRepository) || strings.ContainsAny(result.ProjectRepository, `/\\`) ||
		!projectOperationTargetPattern.MatchString(result.ProjectTarget) || (result.ProjectState != "ready" && result.ProjectState != "dirty") ||
		(result.ProjectProfile != "linux-workcell" && result.ProjectProfile != "windows-workcell") || result.ProjectMode != "dev" ||
		!validProjectToolchainSummary(result) {
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
	metadata.ProjectReason = ""
	metadata.ProjectDiagnosticReason = ""
	metadata.ProjectRepairable = false
	metadata.ProjectRecommendedAction = ""
	metadata.ProjectToolchainState = ""
	metadata.ProjectToolchainRoute = ""
	metadata.ProjectToolchainManifests = nil
	return emptyOperationResult(metadata)
}

func validProjectDiagnosticResult(result OperationResult) bool {
	if !projectOperationAliasPattern.MatchString(result.ProjectAlias) || !projectOperationTargetPattern.MatchString(result.ProjectTarget) ||
		!regexp.MustCompile(`^(absent|blocked|unavailable|timeout|identity_mismatch|corrupt|unsafe_boundary)$`).MatchString(result.ProjectState) ||
		!regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(result.ProjectReason) ||
		(result.ProjectDiagnosticReason != "" && !regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(result.ProjectDiagnosticReason)) ||
		(result.ProjectRecommendedAction != "" && !regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`).MatchString(result.ProjectRecommendedAction)) {
		return false
	}
	if result.WorkspaceID != "" || result.ProjectProfile != "" || result.ProjectMode != "" || hasProjectToolchainSummary(result) {
		return false
	}
	if result.ProjectOwner != "" && !githubOwnerOperationPattern.MatchString(result.ProjectOwner) {
		return false
	}
	if result.ProjectRepository != "" && (!projectOperationRepositoryPattern.MatchString(result.ProjectRepository) || strings.ContainsAny(result.ProjectRepository, `/\\`)) {
		return false
	}
	metadata := result
	metadata.ProjectAlias, metadata.ProjectOwner, metadata.ProjectRepository, metadata.ProjectTarget, metadata.ProjectState = "", "", "", "", ""
	metadata.ProjectReason, metadata.ProjectDiagnosticReason, metadata.ProjectRecommendedAction = "", "", ""
	metadata.ProjectRepairable = false
	return emptyOperationResult(metadata)
}

func hasProjectToolchainSummary(result OperationResult) bool {
	return result.ProjectToolchainState != "" || result.ProjectToolchainRoute != "" || len(result.ProjectToolchainManifests) > 0
}

func validProjectToolchainSummary(result OperationResult) bool {
	if !hasProjectToolchainSummary(result) {
		return true
	}
	wantRoute := map[string]string{
		"supported":     "l3",
		"edge-required": "edge-toolbox",
		"pin-conflict":  "resolve-pins",
	}[result.ProjectToolchainState]
	if wantRoute == "" || result.ProjectToolchainRoute != wantRoute || len(result.ProjectToolchainManifests) > 16 {
		return false
	}
	allowed := map[string]bool{
		"rust-toolchain.toml": true, ".tool-versions": true, "mise.toml": true,
		"package.json": true, "go.mod": true, "pyproject.toml": true, "Cargo.toml": true,
		"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
		"settings.gradle": true, "settings.gradle.kts": true, "CMakeLists.txt": true, "Makefile": true,
	}
	seen := make(map[string]bool, len(result.ProjectToolchainManifests))
	for _, manifest := range result.ProjectToolchainManifests {
		if !allowed[manifest] || seen[manifest] {
			return false
		}
		seen[manifest] = true
	}
	return true
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
	if hasProjectWorktreeResult(result) || hasProjectExecResult(result) || hasProjectNetworkResult(result) || hasProjectProcessResult(result) {
		return false
	}
	return result.WorkspaceID == "" && result.AuthorizationRevision == 0 && result.JobID == "" && result.JobState == "" && result.ProgressRevision == 0 && result.CycleCount == 0 && result.JobSafeCode == "" && result.Release == "" && result.Commit == "" && result.EdgeProtocolVersion == "" && result.EdgeCatalogHash == "" && result.ManifestStatus == "" && !result.ComponentsCompatible && !result.ServiceActive && result.ServiceState == "" && result.ServiceRestarts == 0 && !result.ServiceRestartsKnown && result.ProcessState == "" && result.LockState == "" && result.Coherence == "" && result.ProcessRelease == "" && result.ProcessCommit == "" && !result.UpdateAvailable && !result.Paired && !result.BubblewrapValid && !result.RootlessValid && result.WorkspaceCount == 0 && !result.ProviderValid && !result.DriverValid && len(result.Blockers) == 0 && result.ProjectAlias == "" && result.ProjectOwner == "" && result.ProjectRepository == "" && result.ProjectTarget == "" && result.ProjectState == "" && result.ProjectProfile == "" && result.ProjectMode == "" && result.ProjectReason == "" && result.ProjectDiagnosticReason == "" && !result.ProjectRepairable && result.ProjectRecommendedAction == "" && result.ProjectRegistryAction == "" && result.ProjectClaimGeneration == 0 && len(result.ProjectClaims) == 0 && !hasProjectToolchainSummary(result) && !hasProjectGitHubResult(result)
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
