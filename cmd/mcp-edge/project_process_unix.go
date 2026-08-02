//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectProcess(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	if processes == nil {
		return edge.OperationResult{}, "project_process_unavailable"
	}
	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	var snapshot edgeclient.ProjectProcessSnapshot
	switch operation.Kind {
	case edge.OperationProjectProcessStart:
		snapshot, _, err = processes.Start(ctx, edgeclient.ProjectProcessStartRequest{
			OperationID: operation.ID, IdempotencyKey: operation.Request.IdempotencyKey,
			ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace,
			Argv: operation.Request.Argv, CWD: operation.Request.CWD, Stdin: operation.Request.Stdin, Environment: operation.Request.Environment,
		})
	case edge.OperationProjectProcessStatus:
		snapshot, err = processes.Status(edgeclient.ProjectProcessReadRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
			StdoutOffset: operation.Request.StdoutOffset, StderrOffset: operation.Request.StderrOffset, LimitBytes: operation.Request.OutputLimit,
		})
	case edge.OperationProjectProcessStop:
		snapshot, err = processes.Stop(ctx, edgeclient.ProjectProcessStopRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
			GracePeriod: time.Duration(operation.Request.GraceSeconds) * time.Second,
		})
	case edge.OperationProjectProcessSignal:
		snapshot, err = processes.Signal(edgeclient.ProjectProcessSignalRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
			Signal: edgeclient.ProjectProcessSignal(operation.Request.BackgroundSignal),
		})
	case edge.OperationProjectProcessList:
		items, listErr := processes.List(edgeclient.ProjectProcessListRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Limit: operation.Request.ProcessLimit})
		if listErr != nil {
			err = listErr
			break
		}
		result := projectProcessBaseResult(resolved)
		result.BackgroundProcesses = make([]edge.BackgroundProcessSummary, 0, len(items))
		for _, item := range items {
			result.BackgroundProcesses = append(result.BackgroundProcesses, projectProcessSummary(item))
		}
		return result, ""
	case edge.OperationProjectProcessCleanup:
		cleanup, cleanupErr := processes.Cleanup(edgeclient.ProjectProcessCleanupRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias})
		if cleanupErr != nil {
			err = cleanupErr
			break
		}
		result := projectProcessBaseResult(resolved)
		result.BackgroundCleanupRemoved = cleanup.Removed
		result.BackgroundCleanupActive = cleanup.Active
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
		case errors.Is(err, context.Canceled):
			return edge.OperationResult{}, "cancelled"
		default:
			return edge.OperationResult{}, "project_process_failed"
		}
	}
	return projectProcessOperationResult(resolved, snapshot), ""
}

func projectProcessOperationResult(resolved edgeclient.ProjectResolution, snapshot edgeclient.ProjectProcessSnapshot) edge.OperationResult {
	result := projectProcessBaseResult(resolved)
	result.BackgroundProcessID = snapshot.ProcessID
	result.BackgroundProcessState = string(snapshot.State)
	result.BackgroundStartedAt = snapshot.StartedAt.UTC().Format(time.RFC3339Nano)
	result.BackgroundExitKnown = snapshot.ExitKnown
	result.BackgroundExitCode = snapshot.ExitCode
	result.BackgroundTerminalSignal = string(snapshot.TerminalSignal)
	result.BackgroundReason = snapshot.Reason
	result.BackgroundStdout = snapshot.Stdout
	result.BackgroundStderr = snapshot.Stderr
	result.BackgroundStdoutNext = snapshot.StdoutNext
	result.BackgroundStderrNext = snapshot.StderrNext
	result.BackgroundStdoutEOF = snapshot.StdoutEOF
	result.BackgroundStderrEOF = snapshot.StderrEOF
	result.BackgroundStdoutTruncated = snapshot.StdoutTruncated
	result.BackgroundStderrTruncated = snapshot.StderrTruncated
	if !snapshot.FinishedAt.IsZero() {
		result.BackgroundFinishedAt = snapshot.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func projectProcessBaseResult(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{
		WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias,
		ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: "ready",
		ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
	}
}

func projectProcessSummary(snapshot edgeclient.ProjectProcessSnapshot) edge.BackgroundProcessSummary {
	item := edge.BackgroundProcessSummary{
		ProcessID: snapshot.ProcessID, State: string(snapshot.State), StartedAt: snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
		ExitKnown: snapshot.ExitKnown, ExitCode: snapshot.ExitCode, TerminalSignal: string(snapshot.TerminalSignal), Reason: snapshot.Reason,
	}
	if !snapshot.FinishedAt.IsZero() {
		item.FinishedAt = snapshot.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return item
}
