//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows/svc"
)

func executeWindowsControlOperation(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, workspaceCount int, operation edge.Operation) (edge.OperationResult, string) {
	switch operation.Kind {
	case edge.OperationProjectPrepare:
		return executeWindowsProjectPrepare(ctx, stateRoot, operation.Request)
	case edge.OperationProjectStatus:
		return executeWindowsProjectStatus(ctx, stateRoot, operation.Request)
	case edge.OperationProjectRegistryList, edge.OperationProjectReconcile, edge.OperationProjectRelease:
		return executeProjectRegistryRecovery(ctx, stateRoot, operation, openWindowsProjectControlState, safeWindowsProjectFailure)
	case edge.OperationProjectSnapshot:
		return executeWindowsProjectSnapshot(ctx, stateRoot, operation.Request)
	case edge.OperationProjectExec:
		return executeProjectExec(ctx, stateRoot, operation)
	case edge.OperationProjectProcessStart, edge.OperationProjectProcessStatus, edge.OperationProjectProcessStdin, edge.OperationProjectProcessStop, edge.OperationProjectProcessSignal, edge.OperationProjectProcessList, edge.OperationProjectProcessCleanup:
		return executeWindowsProjectProcess(ctx, stateRoot, processes, operation)
	case edge.OperationProjectGitStatus, edge.OperationProjectGitFetch, edge.OperationProjectGitFastForwardPreview, edge.OperationProjectGitFastForward, edge.OperationProjectGitPublishPreview, edge.OperationProjectGitPublish:
		return executeWindowsProjectGitSync(ctx, stateRoot, operation)
	case edge.OperationProjectGitHubStatus:
		return executeWindowsProjectGitHubStatus(ctx, stateRoot, operation)
	case edge.OperationProjectBrowserCreate, edge.OperationProjectBrowserStatus, edge.OperationProjectBrowserList, edge.OperationProjectBrowserRun, edge.OperationProjectBrowserArtifactRead, edge.OperationProjectBrowserClose, edge.OperationProjectBrowserCleanup:
		return edge.OperationResult{}, "project_browser_unavailable_windows"
	case edge.OperationProjectBrowserHarnessStart, edge.OperationProjectBrowserHarnessStatus, edge.OperationProjectBrowserHarnessList, edge.OperationProjectBrowserHarnessStop, edge.OperationProjectBrowserHarnessCleanup, edge.OperationProjectBrowserHarnessArtifactList, edge.OperationProjectBrowserHarnessArtifactRead:
		return edge.OperationResult{}, "project_browser_harness_unavailable_windows"
	case edge.OperationProjectToolboxCreate, edge.OperationProjectToolboxStatus, edge.OperationProjectToolboxExec, edge.OperationProjectToolboxInstall, edge.OperationProjectToolboxCleanup, edge.OperationProjectToolboxRepair, edge.OperationProjectToolboxServiceStart, edge.OperationProjectToolboxServiceStatus, edge.OperationProjectToolboxServiceStop:
		return edge.OperationResult{}, "project_toolbox_unavailable_windows"
	case edge.OperationLabPrepare, edge.OperationLabRetarget, edge.OperationAutopilotStart, edge.OperationAutopilotPause, edge.OperationAutopilotResume, edge.OperationAutopilotCancel:
		return edge.OperationResult{}, "operation_unavailable_windows"
	case edge.OperationBundleUpdate, edge.OperationBundleRollback, edge.OperationEdgeRepair:
		return edge.OperationResult{}, "bundle_mutation_unavailable_windows"
	case edge.OperationBundleStatus, edge.OperationOnboardingStatus:
		return collectWindowsEdgeDiagnostic(workspaceCount)
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

var collectWindowsEdgeDiagnostic = collectWindowsEdgeDiagnosticResult

func collectWindowsEdgeDiagnosticResult(workspaceCount int) (edge.OperationResult, string) {
	snapshot, err := inspectWindowsDoctor()
	if err != nil {
		return edge.OperationResult{}, "diagnostic_unavailable_windows"
	}
	return windowsDiagnosticResult(snapshot, uint32(os.Getpid()), workspaceCount)
}

func windowsDiagnosticResult(snapshot windowsDoctorSnapshot, responderPID uint32, workspaceCount int) (edge.OperationResult, string) {
	if snapshot.ServiceStatus.State != svc.Running || snapshot.ServiceStatus.ProcessId == 0 {
		return edge.OperationResult{}, "diagnostic_service_inactive_windows"
	}
	if responderPID == 0 || snapshot.ServiceStatus.ProcessId != responderPID {
		return edge.OperationResult{}, "diagnostic_process_mismatch_windows"
	}
	paired := snapshot.Identity.DeviceID != ""
	blockers := []string{}
	if !paired {
		blockers = append(blockers, "edge_unpaired")
	}
	return edge.OperationResult{
		Release: snapshot.BundleRelease, Commit: snapshot.BundleCommit,
		EdgeProtocolVersion: snapshot.ProtocolVersion, EdgeCatalogHash: snapshot.CatalogHash,
		ManifestStatus: "valid", ComponentsCompatible: true,
		ServiceActive: true, ServiceState: "active",
		ProcessState: "single", LockState: "held", Coherence: "managed",
		ProcessRelease: snapshot.BundleRelease, ProcessCommit: snapshot.BundleCommit,
		Paired: paired, WorkspaceCount: workspaceCount,
		ProviderValid: true, DriverValid: true, Blockers: blockers,
	}, ""
}

func openWindowsProjectControlState(stateRoot string) (edgeclient.GitHubCredential, *edgeclient.WorkspaceRegistry, *edgeclient.ProjectRegistry, edgeclient.WorkspaceRoots, string) {
	credential, err := edgeclient.LoadGitHubCredential(stateRoot)
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_credential_unavailable"
	}
	roots, err := edgeclient.DefaultWorkspaceRoots()
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_roots_unavailable"
	}
	workspaces, err := edgeclient.OpenWorkspaceRegistryWithRoots(stateRoot, roots)
	if err != nil {
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_registry_unavailable"
	}
	projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{StateRoot: stateRoot, AllowedOwner: credential.Owner, Workspaces: workspaces})
	if err != nil {
		_ = workspaces.Close()
		return edgeclient.GitHubCredential{}, nil, nil, edgeclient.WorkspaceRoots{}, "project_registry_unavailable"
	}
	return credential, workspaces, projects, roots, ""
}

func executeWindowsProjectPrepare(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	credential, workspaces, projects, roots, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	config := edgeclient.ProjectPreparationConfig{StateRoot: stateRoot, Projects: projects, Workspaces: workspaces, Roots: roots, Credential: credential, Runner: edgeclient.NewDevGitCommandRunner(stateRoot, "")}
	plan, err := edgeclient.PlanProjectPreparation(ctx, config, windowsProjectPreparationRequest(request))
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	if _, err := edgeclient.ApplyProjectPreparation(ctx, config, plan); err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	return windowsProjectControlResult(ctx, projects, request.Alias, request.TargetAlias, false)
}

func windowsProjectPreparationRequest(request edge.OperationRequest) edgeclient.ProjectPreparationRequest {
	return edgeclient.ProjectPreparationRequest{
		Alias:       request.Alias,
		Repository:  request.Repository,
		TargetAlias: request.TargetAlias,
		Profile:     edgeclient.WorkspaceProfileWindowsWorkcell,
	}
}

func executeWindowsProjectStatus(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	_, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	return windowsProjectControlResult(ctx, projects, request.Alias, request.TargetAlias, true)
}

func executeWindowsProjectSnapshot(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	_, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, request.Alias, request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	runner := edgeclient.NewDevGitCommandRunner(stateRoot, "")
	if runner == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileWindowsWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev {
		return edge.OperationResult{}, "project_snapshot_invalid"
	}
	head, err := runner.Run(ctx, resolved.Workspace.Path, []string{"rev-parse", "--verify", "HEAD"}, edgeclient.GitHubCredential{})
	if err != nil || !windowsGitCommit(strings.TrimSpace(head)) {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	branch, err := runner.Run(ctx, resolved.Workspace.Path, []string{"branch", "--show-current"}, edgeclient.GitHubCredential{})
	if err != nil || !windowsGitBranch(strings.TrimSpace(branch)) {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	status, err := runner.Run(ctx, resolved.Workspace.Path, edgeclient.ProjectCheckoutStatusArgs(), edgeclient.GitHubCredential{})
	if err != nil {
		return edge.OperationResult{}, "project_snapshot_failed"
	}
	clean := edgeclient.ProjectCheckoutStatusClean(status)
	state := string(edgeclient.ProjectCheckoutReady)
	if !clean {
		state = string(edgeclient.ProjectCheckoutDirty)
	}
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: state, ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode), SnapshotBranch: strings.TrimSpace(branch), SnapshotHead: strings.TrimSpace(head), SnapshotClean: clean}, ""
}

func windowsProjectControlResult(ctx context.Context, projects *edgeclient.ProjectRegistry, alias, target string, includeToolchain bool) (edge.OperationResult, string) {
	if includeToolchain {
		status, statusErr := projects.Status(ctx, alias, target)
		if statusErr != nil {
			return edge.OperationResult{}, safeWindowsProjectFailure(statusErr)
		}
		if status.State != string(edgeclient.ProjectCheckoutReady) && status.State != string(edgeclient.ProjectCheckoutDirty) {
			return projectStatusOperationResult(status), ""
		}
	}
	resolved, err := projects.Resolve(ctx, alias, target)
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	result := edge.OperationResult{
		WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias,
		ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(),
		ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
	}
	if includeToolchain {
		toolchain, err := edgeclient.DetectToolchainReadiness(resolved.Workspace.Path)
		if err != nil {
			return edge.OperationResult{}, "project_toolchain_manifest_invalid"
		}
		result.ProjectToolchainState = string(toolchain.Status)
		result.ProjectToolchainRoute = map[edgeclient.ToolchainReadinessStatus]string{
			edgeclient.ToolchainSupported:    "l3",
			edgeclient.ToolchainEdgeRequired: "edge-toolbox",
			edgeclient.ToolchainPinConflict:  "resolve-pins",
		}[toolchain.Status]
		result.ProjectToolchainManifests = append([]string(nil), toolchain.Manifests...)
	}
	return result, ""
}

func safeWindowsProjectFailure(err error) string {
	if err == nil {
		return "project_operation_failed"
	}
	var projectFailure *edgeclient.ProjectError
	if errors.As(err, &projectFailure) {
		code := string(projectFailure.Code)
		if code != "" {
			return "project_" + code
		}
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "project_operation_failed"
}

func executeWindowsProjectProcess(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	return executeProjectProcess(ctx, stateRoot, processes, operation)
}

func windowsProjectProcessBase(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}
}

func windowsProjectProcessListResult(resolved edgeclient.ProjectResolution, snapshots []edgeclient.ProjectProcessSnapshot) edge.OperationResult {
	result := windowsProjectProcessBase(resolved)
	result.BackgroundProcesses = make([]edge.BackgroundProcessSummary, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result.BackgroundProcesses = append(result.BackgroundProcesses, projectProcessSummary(snapshot))
	}
	return result
}

func windowsProjectProcessResult(resolved edgeclient.ProjectResolution, snapshot edgeclient.ProjectProcessSnapshot) edge.OperationResult {
	result := windowsProjectProcessBase(resolved)
	result.BackgroundProcessID, result.BackgroundProcessState = snapshot.ProcessID, string(snapshot.State)
	result.BackgroundStartedAt, result.BackgroundExitKnown, result.BackgroundExitCode = snapshot.StartedAt.UTC().Format(time.RFC3339Nano), snapshot.ExitKnown, snapshot.ExitCode
	result.BackgroundTerminalSignal, result.BackgroundReason = string(snapshot.TerminalSignal), snapshot.Reason
	result.BackgroundStdout, result.BackgroundStderr = snapshot.Stdout, snapshot.Stderr
	result.BackgroundStdoutNext, result.BackgroundStderrNext = snapshot.StdoutNext, snapshot.StderrNext
	result.BackgroundStdoutEOF, result.BackgroundStderrEOF = snapshot.StdoutEOF, snapshot.StderrEOF
	result.BackgroundStdoutTruncated, result.BackgroundStderrTruncated = snapshot.StdoutTruncated, snapshot.StderrTruncated
	if !snapshot.FinishedAt.IsZero() {
		result.BackgroundFinishedAt = snapshot.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func windowsGitCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func windowsGitBranch(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && !strings.Contains(value, "..") && !strings.Contains(value, "//") && !strings.Contains(value, "@{") && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, ".") && !strings.HasSuffix(value, ".lock")
}
