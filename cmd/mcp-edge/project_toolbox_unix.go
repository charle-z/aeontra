//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type projectToolboxOperations interface {
	Create(context.Context, edgeclient.ProjectToolboxCreateRequest) (edgeclient.ProjectToolboxSnapshot, bool, error)
	Status(context.Context, edgeclient.ProjectToolboxStatusRequest) (edgeclient.ProjectToolboxSnapshot, error)
	Exec(context.Context, edgeclient.ProjectToolboxExecRequest) (edgeclient.ProjectToolboxSnapshot, error)
	Cleanup(context.Context, edgeclient.ProjectToolboxCleanupRequest) (bool, error)
}

func executeProjectToolbox(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
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
	endpoint, err := edgeclient.DiscoverRootlessContainerEndpoint(os.Geteuid(), "")
	if err != nil || endpoint == nil {
		return edge.OperationResult{}, "project_toolbox_unavailable"
	}
	manager, err := edgeclient.OpenProjectToolboxManager(edgeclient.ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: endpoint})
	if err != nil {
		return edge.OperationResult{}, "project_toolbox_unavailable"
	}
	return collectProjectToolbox(ctx, manager, resolved, operation)
}

func collectProjectToolbox(ctx context.Context, manager projectToolboxOperations, resolved edgeclient.ProjectResolution, operation edge.Operation) (edge.OperationResult, string) {
	request := operation.Request
	var snapshot edgeclient.ProjectToolboxSnapshot
	var err error
	switch operation.Kind {
	case edge.OperationProjectToolboxCreate:
		snapshot, _, err = manager.Create(ctx, edgeclient.ProjectToolboxCreateRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace})
	case edge.OperationProjectToolboxStatus:
		snapshot, err = manager.Status(ctx, edgeclient.ProjectToolboxStatusRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace})
	case edge.OperationProjectToolboxExec, edge.OperationProjectToolboxInstall:
		execCtx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
		snapshot, err = manager.Exec(execCtx, edgeclient.ProjectToolboxExecRequest{
			ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace,
			Argv: request.Argv, CWD: request.CWD, Environment: request.Environment,
		})
		cancel()
	case edge.OperationProjectToolboxCleanup:
		snapshot, err = manager.Status(ctx, edgeclient.ProjectToolboxStatusRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace})
		if err == nil {
			var removed bool
			removed, err = manager.Cleanup(ctx, edgeclient.ProjectToolboxCleanupRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace})
			if removed {
				snapshot.State = edgeclient.ProjectToolboxState("removed")
			}
		}
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
	if err != nil {
		switch {
		case errors.Is(err, edgeclient.ErrProjectToolboxNotFound):
			return edge.OperationResult{}, "project_toolbox_not_found"
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return edge.OperationResult{}, "cancelled"
		default:
			return edge.OperationResult{}, "project_toolbox_failed"
		}
	}
	result := projectProcessBaseResult(resolved)
	result.ToolboxID = snapshot.ToolboxID
	result.ToolboxState = string(snapshot.State)
	result.ToolboxBase = "debian-bookworm-slim"
	result.ToolboxBaseImageID = snapshot.BaseImageID
	result.ToolboxCreatedAt = snapshot.CreatedAt.UTC().Format(time.RFC3339Nano)
	result.ToolboxUpdatedAt = snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)
	result.ToolboxOutput = snapshot.Output
	result.ToolboxOutputTruncated = snapshot.Truncated
	result.ToolboxRemoved = snapshot.State == "removed"
	return result, ""
}
