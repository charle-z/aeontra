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
	result := edge.OperationResult{
		WorkspaceID:  resolved.Workspace.ID,
		ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: "ready", ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		BackgroundProcessID: snapshot.ProcessID, BackgroundProcessState: string(snapshot.State),
		BackgroundStartedAt: snapshot.StartedAt.UTC().Format(time.RFC3339Nano),
		BackgroundExitKnown: snapshot.ExitKnown, BackgroundExitCode: snapshot.ExitCode,
		BackgroundTerminalSignal: string(snapshot.TerminalSignal), BackgroundReason: snapshot.Reason,
		BackgroundStdout: snapshot.Stdout, BackgroundStderr: snapshot.Stderr,
		BackgroundStdoutNext: snapshot.StdoutNext, BackgroundStderrNext: snapshot.StderrNext,
		BackgroundStdoutEOF: snapshot.StdoutEOF, BackgroundStderrEOF: snapshot.StderrEOF,
		BackgroundStdoutTruncated: snapshot.StdoutTruncated, BackgroundStderrTruncated: snapshot.StderrTruncated,
	}
	if !snapshot.FinishedAt.IsZero() {
		result.BackgroundFinishedAt = snapshot.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}
