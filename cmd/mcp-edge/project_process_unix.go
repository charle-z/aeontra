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
	// Starting a process still attests the repository before authorizing a new
	// effect. Existing process reads/effects use only the durable project
	// binding; a normal build or a dirty/slow Git checkout must not make an
	// already-authorized process unobservable or unstoppable.
	var resolved edgeclient.ProjectResolution
	var workspaceRoots edgeclient.WorkspaceRoots
	var err error
	if operation.Kind == edge.OperationProjectProcessStart {
		_, workspaces, projects, roots, code := openProjectControlState(stateRoot)
		if code != "" {
			return edge.OperationResult{}, code
		}
		defer workspaces.Close()
		defer projects.Close()
		workspaceRoots = roots
		resolved, err = projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	} else {
		// A durable process is already authorized. Do not consult project
		// discovery or Git to observe/stop it: registry release/reassociation
		// must not strand a live process. Bind the request to the journal record
		// and verify the caller's alias/target before issuing the effect.
		if operation.Request.BackgroundProcessID != "" {
			binding, bindingErr := processes.Binding(operation.Request.BackgroundProcessID)
			if bindingErr != nil || binding.ProjectAlias != operation.Request.Alias || binding.TargetAlias != operation.Request.TargetAlias {
				return edge.OperationResult{}, "project_process_not_found"
			}
			resolved = durableProjectProcessResolution(binding)
		} else {
			// Listing or cleaning all terminal records still needs the current
			// registry binding so a reassociated alias cannot reach processes
			// from its former workspace. ResolveRegistered performs only the
			// durable boundary/attestation check; a dirty source tree remains
			// valid and does not block process observation or cleanup.
			_, workspaces, projects, _, code := openProjectControlState(stateRoot)
			if code != "" {
				return edge.OperationResult{}, code
			}
			defer workspaces.Close()
			defer projects.Close()
			resolved, err = projects.ResolveRegistered(operation.Request.Alias, operation.Request.TargetAlias)
		}
	}
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	var snapshot edgeclient.ProjectProcessSnapshot
	switch operation.Kind {
	case edge.OperationProjectProcessStart:
		snapshot, _, err = processes.Start(ctx, edgeclient.ProjectProcessStartRequest{
			OperationID: operation.ID, IdempotencyKey: operation.Request.IdempotencyKey,
			ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectClaimGeneration: resolved.Project.ClaimGeneration, ProjectState: resolved.SafeState(), Workspace: resolved.Workspace,
			WorkspaceRoots: workspaceRoots, Argv: operation.Request.Argv, CWD: operation.Request.CWD, Stdin: operation.Request.Stdin, Environment: operation.Request.Environment,
		})
	case edge.OperationProjectProcessStatus:
		snapshot, err = processes.Status(edgeclient.ProjectProcessReadRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID,
			StdoutOffset: operation.Request.StdoutOffset, StderrOffset: operation.Request.StderrOffset, LimitBytes: operation.Request.OutputLimit,
		})
	case edge.OperationProjectProcessStdin:
		var receipt edgeclient.ProjectProcessStdinReceipt
		snapshot, receipt, err = processes.WriteStdin(edgeclient.ProjectProcessStdinRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID,
			FrameID:        operation.Request.IdempotencyKey,
			ExpectedOffset: operation.Request.ProcessStdinOffset, Data: operation.Request.Stdin, Close: operation.Request.ProcessStdinClose,
		})
		if err == nil {
			result := projectProcessOperationResult(resolved, snapshot)
			result.BackgroundStdinNext = receipt.NextOffset
			result.BackgroundStdinAccepted = receipt.AcceptedBytes
			result.BackgroundStdinClosed = receipt.Closed
			result.BackgroundStdinReused = receipt.Reused
			return result, ""
		}
	case edge.OperationProjectProcessStop:
		snapshot, err = processes.Stop(ctx, edgeclient.ProjectProcessStopRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID,
			GracePeriod: time.Duration(operation.Request.GraceSeconds) * time.Second,
		})
	case edge.OperationProjectProcessSignal:
		snapshot, err = processes.Signal(edgeclient.ProjectProcessSignalRequest{
			ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID,
			Signal: edgeclient.ProjectProcessSignal(operation.Request.BackgroundSignal),
		})
	case edge.OperationProjectProcessList:
		items, listErr := processes.List(edgeclient.ProjectProcessListRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID, Limit: operation.Request.ProcessLimit})
		if listErr != nil {
			err = listErr
			break
		}
		result := projectProcessBaseResult(resolved)
		if len(items) > 0 {
			result = projectProcessBaseResult(durableProjectProcessResolutionFromSnapshot(items[0]))
		}
		result.BackgroundProcesses = make([]edge.BackgroundProcessSummary, 0, len(items))
		for _, item := range items {
			result.BackgroundProcesses = append(result.BackgroundProcesses, projectProcessSummary(item))
		}
		return result, ""
	case edge.OperationProjectProcessCleanup:
		cleanup, cleanupErr := processes.Cleanup(edgeclient.ProjectProcessCleanupRequest{ProcessID: operation.Request.BackgroundProcessID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, WorkspaceID: resolved.Workspace.ID})
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
		case errors.Is(err, edgeclient.ErrProjectProcessStdinConflict):
			return edge.OperationResult{}, "project_process_stdin_conflict"
		case errors.Is(err, edgeclient.ErrProjectProcessStdinClosed):
			return edge.OperationResult{}, "project_process_stdin_closed"
		case errors.Is(err, context.Canceled):
			return edge.OperationResult{}, "cancelled"
		default:
			return edge.OperationResult{}, "project_process_failed"
		}
	}
	return projectProcessOperationResult(resolved, snapshot), ""
}

func projectProcessOperationResult(resolved edgeclient.ProjectResolution, snapshot edgeclient.ProjectProcessSnapshot) edge.OperationResult {
	if resolved.RegisteredOnly && snapshot.ProjectAlias != "" {
		resolved = durableProjectProcessResolutionFromSnapshot(snapshot)
	}
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

func durableProjectProcessResolution(binding edgeclient.ProjectProcessBinding) edgeclient.ProjectResolution {
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: binding.ProjectAlias, Owner: binding.ProjectOwner, Repository: binding.ProjectRepository, ClaimGeneration: binding.ProjectClaimGeneration},
		TargetAlias: binding.TargetAlias, Workspace: edgeclient.Workspace{ID: binding.WorkspaceID, Profile: edgeclient.WorkspaceProfile(binding.ProjectProfile), Mode: edgeclient.WorkspaceMode(binding.ProjectMode)},
		CheckoutState: edgeclient.ProjectCheckoutState(binding.ProjectState),
	}
}

func durableProjectProcessResolutionFromSnapshot(snapshot edgeclient.ProjectProcessSnapshot) edgeclient.ProjectResolution {
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: snapshot.ProjectAlias, Owner: snapshot.ProjectOwner, Repository: snapshot.ProjectRepository, ClaimGeneration: snapshot.ProjectClaimGeneration},
		TargetAlias: snapshot.TargetAlias, Workspace: edgeclient.Workspace{ID: snapshot.WorkspaceID, Profile: edgeclient.WorkspaceProfile(snapshot.ProjectProfile), Mode: edgeclient.WorkspaceMode(snapshot.ProjectMode)},
		CheckoutState: edgeclient.ProjectCheckoutState(snapshot.ProjectState),
	}
}

func projectProcessBaseResult(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{
		WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias,
		ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(),
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
