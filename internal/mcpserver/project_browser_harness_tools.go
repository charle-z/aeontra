package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectBrowserHarnessStartParams struct {
	Alias          string            `json:"alias"`
	Target         string            `json:"target"`
	IdempotencyKey string            `json:"idempotency_key"`
	Profile        string            `json:"profile"`
	Argv           []string          `json:"argv"`
	CWD            string            `json:"cwd,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	StorageMiB     int               `json:"storage_mib"`
}
type projectBrowserHarnessStatusParams struct {
	Alias        string `json:"alias"`
	Target       string `json:"target"`
	RunID        string `json:"run_id"`
	StdoutOffset int64  `json:"stdout_offset"`
	StderrOffset int64  `json:"stderr_offset"`
	LimitBytes   int    `json:"limit_bytes"`
}
type projectBrowserHarnessListParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
	Limit  int    `json:"limit"`
}
type projectBrowserHarnessStopParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	RunID          string `json:"run_id"`
	IdempotencyKey string `json:"idempotency_key"`
	GraceSeconds   int    `json:"grace_seconds"`
}
type projectBrowserHarnessCleanupParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	RunID          string `json:"run_id,omitempty"`
	RemoveProfile  bool   `json:"remove_profile,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}
type projectBrowserHarnessArtifactListParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
	RunID  string `json:"run_id"`
	Limit  int    `json:"limit"`
}
type projectBrowserHarnessArtifactReadParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
	RunID  string `json:"run_id"`
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Limit  int    `json:"limit"`
}

type projectBrowserHarnessPublicView struct {
	OperationID        string                               `json:"operation_id"`
	State              edge.OperationState                  `json:"state"`
	Alias              string                               `json:"alias"`
	Repository         string                               `json:"repository,omitempty"`
	Target             string                               `json:"target"`
	Profile            string                               `json:"profile,omitempty"`
	Mode               string                               `json:"mode,omitempty"`
	Reused             bool                                 `json:"reused"`
	Reason             string                               `json:"reason,omitempty"`
	RunID              string                               `json:"run_id,omitempty"`
	RunState           string                               `json:"run_state,omitempty"`
	BrowserProfile     string                               `json:"browser_profile,omitempty"`
	CreatedAt          string                               `json:"created_at,omitempty"`
	UpdatedAt          string                               `json:"updated_at,omitempty"`
	StartedAt          string                               `json:"started_at,omitempty"`
	FinishedAt         string                               `json:"finished_at,omitempty"`
	ExitKnown          bool                                 `json:"exit_known"`
	ExitCode           int                                  `json:"exit_code"`
	TimeoutSeconds     int                                  `json:"timeout_seconds,omitempty"`
	StorageMiB         int                                  `json:"storage_mib,omitempty"`
	Stdout             string                               `json:"stdout,omitempty"`
	Stderr             string                               `json:"stderr,omitempty"`
	StdoutNext         int64                                `json:"stdout_next,omitempty"`
	StderrNext         int64                                `json:"stderr_next,omitempty"`
	StdoutEOF          bool                                 `json:"stdout_eof"`
	StderrEOF          bool                                 `json:"stderr_eof"`
	StdoutTruncated    bool                                 `json:"stdout_truncated"`
	StderrTruncated    bool                                 `json:"stderr_truncated"`
	ArtifactCount      int                                  `json:"artifact_count,omitempty"`
	ArtifactBytes      int64                                `json:"artifact_bytes,omitempty"`
	Runs               []edge.BrowserHarnessSummary         `json:"runs,omitempty"`
	Artifacts          []edge.BrowserHarnessArtifactSummary `json:"artifacts,omitempty"`
	ArtifactPath       string                               `json:"artifact_path,omitempty"`
	ArtifactMediaType  string                               `json:"artifact_media_type,omitempty"`
	ArtifactSHA256     string                               `json:"artifact_sha256,omitempty"`
	ArtifactOffset     int64                                `json:"artifact_offset,omitempty"`
	ArtifactNext       int64                                `json:"artifact_next,omitempty"`
	ArtifactEOF        bool                                 `json:"artifact_eof"`
	ArtifactDataBase64 string                               `json:"artifact_data_base64,omitempty"`
	CleanupRuns        int                                  `json:"cleanup_runs,omitempty"`
	CleanupArtifacts   int                                  `json:"cleanup_artifacts,omitempty"`
	CleanupProfiles    int                                  `json:"cleanup_profiles,omitempty"`
}

func (s *Server) addProjectBrowserHarnessTools(projectSchema map[string]any) {
	common := map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}
	key := stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128)
	runID := stringSchema("opaque durable browser harness run id", `^bh_[a-f0-9]{32}$`, 35)
	profile := stringSchema("persistent browser profile name", `^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`, 64)
	argv := map[string]any{"type": "array", "minItems": 1, "maxItems": 128, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 8192}}
	environment := map[string]any{"type": "object", "maxProperties": 32, "propertyNames": map[string]any{"pattern": `^[A-Za-z_][A-Za-z0-9_]{0,63}$`}, "additionalProperties": map[string]any{"type": "string", "maxLength": 4096}}
	start := copySchema(common)
	start["idempotency_key"] = key
	start["profile"] = profile
	start["argv"] = argv
	start["cwd"] = map[string]any{"type": "string", "maxLength": 1024}
	start["environment"] = environment
	start["timeout_seconds"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 604800}
	start["storage_mib"] = map[string]any{"type": "integer", "minimum": 64, "maximum": 65536}
	s.addDirectTool(toolDef{Name: "project_browser_harness_start", Description: "Start or reuse one durable arbitrary argv inside the project's persistent rootless toolbox. The command may be any Playwright, Puppeteer, Selenium, WebDriver, shell-free language runtime or browser automation framework installed in the toolbox. The workspace is /workspace; managed run, artifact, download and persistent profile directories are supplied through environment variables; storage_mib bounds the combined run and profile state. No browser action allowlist, JavaScript restriction, domain allowlist or fixed browser is imposed.", InputSchema: closedObject(start, []string{"alias", "target", "idempotency_key", "profile", "argv", "timeout_seconds", "storage_mib"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}}, s.handleProjectBrowserHarnessStart)
	status := copySchema(common)
	status["run_id"] = runID
	status["stdout_offset"] = map[string]any{"type": "integer", "minimum": 0}
	status["stderr_offset"] = map[string]any{"type": "integer", "minimum": 0}
	status["limit_bytes"] = map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxBrowserHarnessLogBytes}
	s.addDirectTool(toolDef{Name: "project_browser_harness_status", Description: "Read durable browser harness lifecycle plus bounded incremental redacted stdout/stderr and aggregate artifact metadata. It never returns argv, environment, host paths, process ids, cookies or profile contents.", InputSchema: closedObject(status, []string{"alias", "target", "run_id", "stdout_offset", "stderr_offset", "limit_bytes"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessStatus)
	list := copySchema(common)
	list["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxBrowserHarnessRuns}
	s.addDirectTool(toolDef{Name: "project_browser_harness_list", Description: "List bounded durable browser harness run metadata for one authorized development workspace.", InputSchema: closedObject(list, []string{"alias", "target", "limit"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessList)
	stop := copySchema(common)
	stop["run_id"] = runID
	stop["idempotency_key"] = key
	stop["grace_seconds"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 30}
	s.addDirectTool(toolDef{Name: "project_browser_harness_stop", Description: "Idempotently stop one owned browser harness process tree after run identity revalidation and a bounded grace period.", InputSchema: closedObject(stop, []string{"alias", "target", "run_id", "idempotency_key", "grace_seconds"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessStop)
	cleanup := copySchema(common)
	cleanup["run_id"] = runID
	cleanup["remove_profile"] = map[string]any{"type": "boolean"}
	cleanup["idempotency_key"] = key
	s.addDirectTool(toolDef{Name: "project_browser_harness_cleanup", Description: "Remove only terminal managed harness run directories and metadata. A selected persistent profile is removed only when explicitly requested and no retained run still uses it.", InputSchema: closedObject(cleanup, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessCleanup)
	artifactList := copySchema(common)
	artifactList["run_id"] = runID
	artifactList["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxBrowserHarnessArtifacts}
	s.addDirectTool(toolDef{Name: "project_browser_harness_artifact_list", Description: "List bounded metadata for arbitrary regular files produced under the managed artifacts or downloads directories, including screenshots, PDFs, traces, videos, HARs and downloaded files.", InputSchema: closedObject(artifactList, []string{"alias", "target", "run_id", "limit"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessArtifactList)
	artifactRead := copySchema(common)
	artifactRead["run_id"] = runID
	artifactRead["path"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 1024, "pattern": `^(?:artifacts|downloads)/[^\\x00]+$`}
	artifactRead["offset"] = map[string]any{"type": "integer", "minimum": 0}
	artifactRead["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxBrowserHarnessArtifactChunk}
	s.addDirectTool(toolDef{Name: "project_browser_harness_artifact_read", Description: "Read one exact bounded base64 chunk from any regular managed harness artifact or download. Paths are relative to the run and symlinks or traversal are rejected.", InputSchema: closedObject(artifactRead, []string{"alias", "target", "run_id", "path", "offset", "limit"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserHarnessArtifactRead)
}

func (s *Server) handleProjectBrowserHarnessStart(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessStartParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessStart, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", IdempotencyKey: p.IdempotencyKey, Argv: p.Argv, CWD: p.CWD, Environment: p.Environment, BrowserHarnessProfile: p.Profile, BrowserHarnessTimeoutSeconds: p.TimeoutSeconds, BrowserHarnessStorageMiB: p.StorageMiB})
}
func (s *Server) handleProjectBrowserHarnessStatus(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessStatusParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessStatus, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessRunID: p.RunID, StdoutOffset: p.StdoutOffset, StderrOffset: p.StderrOffset, OutputLimit: p.LimitBytes})
}
func (s *Server) handleProjectBrowserHarnessList(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessListParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessList, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessListLimit: p.Limit})
}
func (s *Server) handleProjectBrowserHarnessStop(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessStopParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessStop, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessRunID: p.RunID, IdempotencyKey: p.IdempotencyKey, GraceSeconds: p.GraceSeconds})
}
func (s *Server) handleProjectBrowserHarnessCleanup(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessCleanupParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessCleanup, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessRunID: p.RunID, BrowserHarnessRemoveProfile: p.RemoveProfile, IdempotencyKey: p.IdempotencyKey})
}
func (s *Server) handleProjectBrowserHarnessArtifactList(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessArtifactListParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessArtifactList, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessRunID: p.RunID, BrowserHarnessListLimit: p.Limit})
}
func (s *Server) handleProjectBrowserHarnessArtifactRead(raw json.RawMessage) (string, error) {
	var p projectBrowserHarnessArtifactReadParams
	if err := decodeClosed(raw, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowserHarness(p.Alias, p.Target, edge.OperationProjectBrowserHarnessArtifactRead, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserHarnessRunID: p.RunID, BrowserHarnessArtifactPath: p.Path, BrowserHarnessArtifactOffset: p.Offset, BrowserHarnessArtifactLimit: p.Limit})
}

func (s *Server) queueProjectBrowserHarness(alias, target string, kind edge.OperationKind, request edge.OperationRequest) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(target)
	if err != nil {
		return "", err
	}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	view := projectBrowserHarnessPublicView{OperationID: operation.ID, State: operation.State, Alias: alias, Target: target, Reused: err == nil && !created}
	if operation.State == edge.OperationSucceeded {
		r := operation.Result
		view.Alias = r.ProjectAlias
		view.Repository = r.ProjectOwner + "/" + r.ProjectRepository
		view.Target = r.ProjectTarget
		view.Profile = r.ProjectProfile
		view.Mode = r.ProjectMode
		view.RunID = r.BrowserHarnessRunID
		view.RunState = r.BrowserHarnessState
		view.BrowserProfile = r.BrowserHarnessProfile
		view.CreatedAt = r.BrowserHarnessCreatedAt
		view.UpdatedAt = r.BrowserHarnessUpdatedAt
		view.StartedAt = r.BrowserHarnessStartedAt
		view.FinishedAt = r.BrowserHarnessFinishedAt
		view.ExitKnown = r.BrowserHarnessExitKnown
		view.ExitCode = r.BrowserHarnessExitCode
		view.TimeoutSeconds = r.BrowserHarnessTimeoutSeconds
		view.StorageMiB = r.BrowserHarnessStorageMiB
		view.Stdout = r.BrowserHarnessStdout
		view.Stderr = r.BrowserHarnessStderr
		view.StdoutNext = r.BrowserHarnessStdoutNext
		view.StderrNext = r.BrowserHarnessStderrNext
		view.StdoutEOF = r.BrowserHarnessStdoutEOF
		view.StderrEOF = r.BrowserHarnessStderrEOF
		view.StdoutTruncated = r.BrowserHarnessStdoutTruncated
		view.StderrTruncated = r.BrowserHarnessStderrTruncated
		view.ArtifactCount = r.BrowserHarnessArtifactCount
		view.ArtifactBytes = r.BrowserHarnessArtifactBytes
		view.Runs = r.BrowserHarnessRuns
		view.Artifacts = r.BrowserHarnessArtifacts
		view.ArtifactPath = r.BrowserHarnessArtifactPath
		view.ArtifactMediaType = r.BrowserHarnessArtifactMediaType
		view.ArtifactSHA256 = r.BrowserHarnessArtifactSHA256
		view.ArtifactBytes = r.BrowserHarnessArtifactBytes
		view.ArtifactOffset = r.BrowserHarnessArtifactOffset
		view.ArtifactNext = r.BrowserHarnessArtifactNext
		view.ArtifactEOF = r.BrowserHarnessArtifactEOF
		view.ArtifactDataBase64 = r.BrowserHarnessArtifactDataBase64
		view.CleanupRuns = r.BrowserHarnessCleanupRuns
		view.CleanupArtifacts = r.BrowserHarnessCleanupArtifacts
		view.CleanupProfiles = r.BrowserHarnessCleanupProfiles
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
