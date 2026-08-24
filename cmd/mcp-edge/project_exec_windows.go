//go:build windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectExec(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	started := time.Now()
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
	return collectProjectExecWithResolution(ctx, operation, resolved, nil, time.Since(started).Microseconds())
}

func collectProjectExec(ctx context.Context, operation edge.Operation, resolved edgeclient.ProjectResolution, runner edgeclient.DirectWorkcellCommandRunner) (edge.OperationResult, string) {
	return collectProjectExecWithResolution(ctx, operation, resolved, runner, 0)
}

func collectProjectExecWithResolution(ctx context.Context, operation edge.Operation, resolved edgeclient.ProjectResolution, runner edgeclient.DirectWorkcellCommandRunner, resolutionUS int64) (edge.OperationResult, string) {
	execution, err := edgeclient.RunDirectWorkcellCommand(ctx, edgeclient.DirectWorkcellCommandRequest{
		OperationID:    operation.ID,
		Workspace:      resolved.Workspace,
		WindowsDevRoot: resolved.Workspace.WindowsDevRoot,
		Argv:           operation.Request.Argv,
		CWD:            operation.Request.CWD,
		Stdin:          operation.Request.Stdin,
		Environment:    operation.Request.Environment,
		TimeoutSeconds: operation.Request.TimeoutSeconds,
	}, runner)
	if err != nil {
		switch {
		case errors.Is(err, edgeclient.ErrDirectWorkcellContract):
			return edge.OperationResult{}, "project_exec_invalid"
		case errors.Is(err, context.Canceled):
			return edge.OperationResult{}, "cancelled"
		default:
			return edge.OperationResult{}, "project_exec_failed"
		}
	}
	return edge.OperationResult{
		WorkspaceID:         resolved.Workspace.ID,
		ProjectAlias:        resolved.Project.Alias,
		ProjectOwner:        resolved.Project.Owner,
		ProjectRepository:   resolved.Project.Repository,
		ProjectTarget:       resolved.TargetAlias,
		ProjectState:        resolved.SafeState(),
		ProjectProfile:      string(resolved.Workspace.Profile),
		ProjectMode:         string(resolved.Workspace.Mode),
		ExecCompleted:       execution.Completed,
		ExecExitCode:        execution.ExitCode,
		ExecStdout:          execution.Stdout,
		ExecStderr:          execution.Stderr,
		ExecTimedOut:        execution.TimedOut,
		ExecStdoutTruncated: execution.StdoutTruncated,
		ExecStderrTruncated: execution.StderrTruncated,
		ExecTimingKnown:     execution.TimingKnown,
		ExecPreflightUS:     execution.PreflightUS + resolutionUS,
		ExecExecutionUS:     execution.ExecutionUS,
		ExecResultUS:        execution.ResultUS,
	}, ""
}
