//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeWindowsControlOperation(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	switch operation.Kind {
	case edge.OperationProjectPrepare:
		return executeWindowsProjectPrepare(ctx, stateRoot, operation.Request)
	case edge.OperationProjectStatus:
		return executeWindowsProjectStatus(ctx, stateRoot, operation.Request)
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
		return edge.OperationResult{}, "diagnostic_unavailable_windows"
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
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
	plan, err := edgeclient.PlanProjectPreparation(ctx, config, edgeclient.ProjectPreparationRequest{Alias: request.Alias, Repository: request.Repository, TargetAlias: request.TargetAlias, Profile: edgeclient.WorkspaceProfile(request.Profile)})
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	if _, err := edgeclient.ApplyProjectPreparation(ctx, config, plan); err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	return windowsProjectControlResult(ctx, projects, request.Alias, request.TargetAlias)
}

func executeWindowsProjectStatus(ctx context.Context, stateRoot string, request edge.OperationRequest) (edge.OperationResult, string) {
	_, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	return windowsProjectControlResult(ctx, projects, request.Alias, request.TargetAlias)
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

func windowsProjectControlResult(ctx context.Context, projects *edgeclient.ProjectRegistry, alias, target string) (edge.OperationResult, string) {
	resolved, err := projects.Resolve(ctx, alias, target)
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}, ""
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
	if processes == nil {
		return edge.OperationResult{}, "project_process_unavailable"
	}
	_, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	var snapshot edgeclient.ProjectProcessSnapshot
	switch operation.Kind {
	case edge.OperationProjectProcessStart:
		snapshot, _, err = processes.Start(ctx, edgeclient.ProjectProcessStartRequest{OperationID: operation.ID, IdempotencyKey: operation.Request.IdempotencyKey, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, Argv: operation.Request.Argv, CWD: operation.Request.CWD, Stdin: operation.Request.Stdin, Environment: operation.Request.Environment})
	case edge.OperationProjectProcessStatus:
		snapshot, err = processes.Status(edgeclient.ProjectProcessReadRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, StdoutOffset: operation.Request.StdoutOffset, StderrOffset: operation.Request.StderrOffset, LimitBytes: operation.Request.OutputLimit})
	case edge.OperationProjectProcessStdin:
		var receipt edgeclient.ProjectProcessStdinReceipt
		snapshot, receipt, err = processes.WriteStdin(edgeclient.ProjectProcessStdinRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, FrameID: operation.Request.IdempotencyKey, ExpectedOffset: operation.Request.ProcessStdinOffset, Data: operation.Request.Stdin, Close: operation.Request.ProcessStdinClose})
		if err == nil {
			result := windowsProjectProcessResult(resolved, snapshot)
			result.BackgroundStdinNext, result.BackgroundStdinAccepted, result.BackgroundStdinClosed, result.BackgroundStdinReused = receipt.NextOffset, receipt.AcceptedBytes, receipt.Closed, receipt.Reused
			return result, ""
		}
	case edge.OperationProjectProcessStop:
		snapshot, err = processes.Stop(ctx, edgeclient.ProjectProcessStopRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, GracePeriod: time.Duration(operation.Request.GraceSeconds) * time.Second})
	case edge.OperationProjectProcessSignal:
		snapshot, err = processes.Signal(edgeclient.ProjectProcessSignalRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Signal: edgeclient.ProjectProcessSignal(operation.Request.BackgroundSignal)})
	case edge.OperationProjectProcessList:
		items, listErr := processes.List(edgeclient.ProjectProcessListRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Limit: operation.Request.ProcessLimit})
		if listErr != nil {
			err = listErr
			break
		}
		result := windowsProjectProcessBase(resolved)
		for _, item := range items {
			result.BackgroundProcesses = append(result.BackgroundProcesses, edge.BackgroundProcessSummary{ProcessID: item.ProcessID, State: string(item.State), StartedAt: item.StartedAt.UTC().Format(time.RFC3339Nano), ExitKnown: item.ExitKnown, ExitCode: item.ExitCode, TerminalSignal: string(item.TerminalSignal), Reason: item.Reason})
		}
		return result, ""
	case edge.OperationProjectProcessCleanup:
		cleanup, cleanupErr := processes.Cleanup(edgeclient.ProjectProcessCleanupRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias})
		if cleanupErr != nil {
			err = cleanupErr
			break
		}
		result := windowsProjectProcessBase(resolved)
		result.BackgroundCleanupRemoved, result.BackgroundCleanupActive = cleanup.Removed, cleanup.Active
		return result, ""
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
	if err != nil {
		switch {
		case errors.Is(err, edgeclient.ErrProjectProcessNotFound):
			return edge.OperationResult{}, "project_process_not_found"
		case errors.Is(err, edgeclient.ErrProjectProcessIdempotencyConflict):
			return edge.OperationResult{}, "project_process_idempotency_conflict"
		case errors.Is(err, edgeclient.ErrProjectProcessStdinConflict):
			return edge.OperationResult{}, "project_process_stdin_conflict"
		case errors.Is(err, context.Canceled):
			return edge.OperationResult{}, "cancelled"
		default:
			return edge.OperationResult{}, "project_process_failed"
		}
	}
	return windowsProjectProcessResult(resolved, snapshot), ""
}

func windowsProjectProcessBase(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}
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
