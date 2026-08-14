//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectWorktree(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	credential, workspaces, projects, roots, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeProjectControlFailure(err)
	}
	manager, err := edgeclient.OpenProjectWorktreeManager(edgeclient.ProjectWorktreeManagerConfig{
		StateRoot: stateRoot, Roots: roots, Workspaces: workspaces,
		Runner: edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"), Credential: credential,
	})
	if err != nil {
		return edge.OperationResult{}, "project_worktree_unavailable"
	}
	defer manager.Close()

	var snapshot edgeclient.ProjectWorktreeSnapshot
	switch operation.Kind {
	case edge.OperationProjectWorktreeCreate:
		snapshot, _, err = manager.Create(ctx, edgeclient.ProjectWorktreeCreateRequest{
			Alias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
			Repository:           resolved.Project.Owner + "/" + resolved.Project.Repository,
			CanonicalWorkspaceID: resolved.Workspace.ID, CanonicalPath: resolved.Workspace.Path,
			BaseCommit: operation.Request.WorktreeBaseCommit, Role: edgeclient.ProjectWorktreeRole(operation.Request.WorktreeRole),
			JobID: operation.Request.WorkJobID, LeaseID: operation.Request.WorkLeaseID, Fence: operation.Request.WorkFence,
			IdempotencyKey: operation.Request.IdempotencyKey,
		})
	case edge.OperationProjectWorktreeClaim:
		snapshot, err = manager.Claim(edgeclient.ProjectWorktreeClaimRequest{
			ID: operation.Request.WorktreeID, JobID: operation.Request.WorkJobID,
			LeaseID: operation.Request.WorkLeaseID, Fence: operation.Request.WorkFence,
		})
	case edge.OperationProjectWorktreeStatus:
		snapshot, err = manager.Status(ctx, operation.Request.WorktreeID)
	case edge.OperationProjectWorktreeList:
		var items []edgeclient.ProjectWorktreeSnapshot
		items, err = manager.List(ctx, resolved.Project.Alias, resolved.TargetAlias, operation.Request.WorktreeLimit)
		if err == nil {
			return projectWorktreeListResult(resolved, items), ""
		}
	case edge.OperationProjectWorktreeCleanup:
		snapshot, _, err = manager.Cleanup(ctx, edgeclient.ProjectWorktreeCleanupRequest{
			ID: operation.Request.WorktreeID, JobID: operation.Request.WorkJobID,
			LeaseID: operation.Request.WorkLeaseID, Fence: operation.Request.WorkFence,
			IdempotencyKey: operation.Request.IdempotencyKey,
		})
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
	if err != nil {
		return edge.OperationResult{}, safeProjectWorktreeFailure(err)
	}
	if snapshot.Alias != resolved.Project.Alias || snapshot.TargetAlias != resolved.TargetAlias || snapshot.Repository != resolved.Project.Owner+"/"+resolved.Project.Repository {
		return edge.OperationResult{}, "project_worktree_conflict"
	}
	return projectWorktreeResult(resolved, snapshot), ""
}

func projectWorktreeResult(resolved edgeclient.ProjectResolution, snapshot edgeclient.ProjectWorktreeSnapshot) edge.OperationResult {
	return edge.OperationResult{
		WorkspaceID:  snapshot.WorkspaceID,
		ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		WorktreeID: snapshot.ID, WorktreeState: string(snapshot.State), WorktreeRole: string(snapshot.Role),
		WorktreeBaseCommit: snapshot.BaseCommit, WorktreeBranch: snapshot.Branch,
		WorkJobID: snapshot.JobID, WorkLeaseID: snapshot.LeaseID, WorkFence: snapshot.Fence,
		WorktreeCreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), WorktreeUpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func projectWorktreeListResult(resolved edgeclient.ProjectResolution, items []edgeclient.ProjectWorktreeSnapshot) edge.OperationResult {
	result := edge.OperationResult{
		WorkspaceID:  resolved.Workspace.ID,
		ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository,
		ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode),
		Worktrees: make([]edge.ProjectWorktreeSummary, 0, len(items)),
	}
	for _, item := range items {
		result.Worktrees = append(result.Worktrees, edge.ProjectWorktreeSummary{
			WorktreeID: item.ID, WorkspaceID: item.WorkspaceID, State: string(item.State), Role: string(item.Role),
			BaseCommit: item.BaseCommit, Branch: item.Branch, JobID: item.JobID, LeaseID: item.LeaseID, Fence: item.Fence,
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return result
}

func safeProjectWorktreeFailure(err error) string {
	switch {
	case errors.Is(err, edgeclient.ErrProjectWorktreeInvalid):
		return "project_worktree_invalid"
	case errors.Is(err, edgeclient.ErrProjectWorktreeNotFound):
		return "project_worktree_not_found"
	case errors.Is(err, edgeclient.ErrProjectWorktreeConflict):
		return "project_worktree_conflict"
	case errors.Is(err, edgeclient.ErrProjectWorktreeStaleFence):
		return "project_worktree_stale_fence"
	case errors.Is(err, edgeclient.ErrProjectWorktreeBaseChanged):
		return "project_worktree_base_changed"
	case errors.Is(err, edgeclient.ErrProjectWorktreeDirty):
		return "project_worktree_dirty"
	case errors.Is(err, edgeclient.ErrProjectWorktreeUnsafe):
		return "project_worktree_unsafe"
	default:
		return "project_worktree_unavailable"
	}
}
