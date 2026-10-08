//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

// executeProjectWorktreeTest maps the bounded acceptance-process operations to the existing managers.
func executeProjectWorktreeTest(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, tests *edgeclient.ProjectWorktreeTestProcessManager, operation edge.Operation) (edge.OperationResult, string) {
	if ctx == nil || processes == nil || tests == nil {
		return edge.OperationResult{}, "project_worktree_test_unavailable"
	}
	request := operation.Request
	switch operation.Kind {
	case edge.OperationProjectWorktreeTestProfile:
		profile, err := tests.Profile(request.TestProfileID)
		if err != nil {
			return edge.OperationResult{}, safeProjectWorktreeTestFailure(err)
		}
		return edge.OperationResult{TestProfileID: profile.ID, TestProfileDigest: profile.Digest, TestTimeoutSeconds: profile.TimeoutSeconds}, ""
	case edge.OperationProjectWorktreeTestStop:
		snapshot, err := tests.Stop(ctx, edgeclient.ProjectWorktreeTestStopRequest{
			ProcessID: request.BackgroundProcessID, Alias: request.Alias, TargetAlias: request.TargetAlias,
			WorktreeID: request.WorktreeID, JobID: request.WorkJobID, LeaseID: request.WorkLeaseID, Fence: request.WorkFence,
			ProfileID: request.TestProfileID, ProfileDigest: request.TestProfileDigest,
		})
		if err != nil {
			return edge.OperationResult{}, safeProjectWorktreeTestFailure(err)
		}
		return projectWorktreeTestOperationResult(snapshot), ""
	case edge.OperationProjectWorktreeTestStart, edge.OperationProjectWorktreeTestStatus:
	default:
		return edge.OperationResult{}, "operation_invalid"
	}

	credential, workspaces, projects, roots, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	worktrees, err := edgeclient.OpenProjectWorktreeManager(edgeclient.ProjectWorktreeManagerConfig{
		StateRoot: stateRoot, Roots: roots, Workspaces: workspaces,
		Runner: edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"), Credential: credential,
	})
	if err != nil {
		return edge.OperationResult{}, "project_worktree_test_unavailable"
	}
	defer worktrees.Close()

	var snapshot edgeclient.ProjectWorktreeTestSnapshot
	switch operation.Kind {
	case edge.OperationProjectWorktreeTestStart:
		snapshot, _, err = tests.Start(ctx, worktrees, edgeclient.ProjectWorktreeTestStartRequest{
			OperationID: operation.ID, IdempotencyKey: request.IdempotencyKey, Alias: request.Alias, TargetAlias: request.TargetAlias,
			WorktreeID: request.WorktreeID, JobID: request.WorkJobID, LeaseID: request.WorkLeaseID, Fence: request.WorkFence,
			ProfileID: request.TestProfileID, ProfileDigest: request.TestProfileDigest,
		})
	case edge.OperationProjectWorktreeTestStatus:
		snapshot, err = tests.Status(ctx, worktrees, edgeclient.ProjectWorktreeTestReadRequest{
			ProcessID: request.BackgroundProcessID, Alias: request.Alias, TargetAlias: request.TargetAlias,
			WorktreeID: request.WorktreeID, JobID: request.WorkJobID, LeaseID: request.WorkLeaseID, Fence: request.WorkFence,
			ProfileID: request.TestProfileID, ProfileDigest: request.TestProfileDigest,
			StdoutOffset: request.StdoutOffset, StderrOffset: request.StderrOffset, LimitBytes: request.OutputLimit,
		})
	}
	if err != nil {
		return edge.OperationResult{}, safeProjectWorktreeTestFailure(err)
	}
	return projectWorktreeTestOperationResult(snapshot), ""
}

func projectWorktreeTestOperationResult(snapshot edgeclient.ProjectWorktreeTestSnapshot) edge.OperationResult {
	result := edge.OperationResult{
		WorkspaceID: snapshot.WorkspaceID,
		WorktreeID:  snapshot.WorktreeID, WorktreeState: string(edgeclient.ProjectWorktreeReady), WorktreeRole: string(edgeclient.ProjectWorktreeWriter),
		WorktreeBaseCommit: snapshot.BaseCommit, WorktreeHeadCommit: snapshot.HeadCommit, WorktreeBranch: snapshot.Branch,
		WorkJobID: snapshot.JobID, WorkLeaseID: snapshot.LeaseID, WorkFence: snapshot.Fence,
		TestProfileID: snapshot.ProfileID, TestProfileDigest: snapshot.ProfileDigest, TestTimeoutSeconds: snapshot.TimeoutSeconds,
		ContentDigest: snapshot.ContentDigest, TestStale: snapshot.Stale, TestStaleReason: snapshot.StaleReason,
		BackgroundProcessID: snapshot.ProcessID, BackgroundProcessState: snapshot.State,
		BackgroundStartedAt: snapshot.StartedAt.UTC().Format(time.RFC3339Nano), BackgroundExitKnown: snapshot.ExitKnown,
		BackgroundExitCode: snapshot.ExitCode, BackgroundTerminalSignal: snapshot.TerminalSignal, BackgroundReason: snapshot.Reason,
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

func safeProjectWorktreeTestFailure(err error) string {
	switch {
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestProfileUnavailable):
		return "project_worktree_test_profile_unavailable"
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestProfileMismatch):
		return "project_worktree_test_profile_mismatch"
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestNotFound):
		return "project_worktree_test_not_found"
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestAttemptExists):
		return "project_worktree_test_attempt_exists"
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestStaleFence), errors.Is(err, edgeclient.ErrProjectWorktreeStaleFence):
		return "project_worktree_test_stale_fence"
	case errors.Is(err, edgeclient.ErrProjectWorktreeTestConflict), errors.Is(err, edgeclient.ErrProjectWorktreeConflict):
		return "project_worktree_test_conflict"
	case errors.Is(err, edgeclient.ErrProjectWorktreeUnsafe):
		return "project_worktree_test_unsafe"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "project_worktree_test_unavailable"
	}
}
