//go:build !windows

package main

import (
	"context"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func collectProjectBrowserHarness(ctx context.Context, manager projectToolboxOperations, resolved edgeclient.ProjectResolution, operation edge.Operation) (edge.OperationResult, string) {
	request := operation.Request
	base := projectProcessBaseResult(resolved)
	switch operation.Kind {
	case edge.OperationProjectBrowserHarnessStart:
		snapshot, _, err := manager.BrowserHarnessStart(ctx, edgeclient.ProjectBrowserHarnessStartRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, IdempotencyKey: request.IdempotencyKey, Profile: request.BrowserHarnessProfile, Argv: request.Argv, CWD: request.CWD, Environment: request.Environment, TimeoutSeconds: request.BrowserHarnessTimeoutSeconds, StorageMiB: request.BrowserHarnessStorageMiB})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		return projectBrowserHarnessSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserHarnessStatus:
		snapshot, err := manager.BrowserHarnessStatus(ctx, edgeclient.ProjectBrowserHarnessStatusRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, RunID: request.BrowserHarnessRunID, StdoutOffset: request.StdoutOffset, StderrOffset: request.StderrOffset, Limit: request.OutputLimit})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		return projectBrowserHarnessSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserHarnessList:
		runs, err := manager.BrowserHarnessList(ctx, edgeclient.ProjectBrowserHarnessListRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, Limit: request.BrowserHarnessListLimit})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		base.BrowserHarnessListComplete = true
		base.BrowserHarnessRuns = make([]edge.BrowserHarnessSummary, 0, len(runs))
		for _, run := range runs {
			base.BrowserHarnessRuns = append(base.BrowserHarnessRuns, projectBrowserHarnessSummaryResult(run))
		}
		return base, ""
	case edge.OperationProjectBrowserHarnessStop:
		snapshot, err := manager.BrowserHarnessStop(ctx, edgeclient.ProjectBrowserHarnessStopRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, RunID: request.BrowserHarnessRunID, GraceSeconds: request.GraceSeconds})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		return projectBrowserHarnessSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserHarnessCleanup:
		cleaned, err := manager.BrowserHarnessCleanup(edgeclient.ProjectBrowserHarnessCleanupRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, RunID: request.BrowserHarnessRunID, RemoveProfile: request.BrowserHarnessRemoveProfile})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		base.BrowserHarnessCleanupComplete = true
		base.BrowserHarnessCleanupRuns = cleaned.Runs
		base.BrowserHarnessCleanupArtifacts = cleaned.Artifacts
		base.BrowserHarnessCleanupProfiles = cleaned.Profiles
		return base, ""
	case edge.OperationProjectBrowserHarnessArtifactList:
		artifacts, err := manager.BrowserHarnessArtifactList(edgeclient.ProjectBrowserHarnessArtifactListRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, RunID: request.BrowserHarnessRunID, Limit: request.BrowserHarnessListLimit})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		base.BrowserHarnessRunID = request.BrowserHarnessRunID
		base.BrowserHarnessArtifactsComplete = true
		base.BrowserHarnessArtifacts = make([]edge.BrowserHarnessArtifactSummary, 0, len(artifacts))
		for _, artifact := range artifacts {
			base.BrowserHarnessArtifacts = append(base.BrowserHarnessArtifacts, edge.BrowserHarnessArtifactSummary{Path: artifact.Path, MediaType: artifact.MediaType, Bytes: artifact.Bytes, SHA256: artifact.SHA256, UpdatedAt: artifact.UpdatedAt.UTC().Format(time.RFC3339Nano)})
		}
		return base, ""
	case edge.OperationProjectBrowserHarnessArtifactRead:
		chunk, err := manager.BrowserHarnessArtifactRead(edgeclient.ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias, Workspace: resolved.Workspace, RunID: request.BrowserHarnessRunID, Path: request.BrowserHarnessArtifactPath, Offset: request.BrowserHarnessArtifactOffset, Limit: request.BrowserHarnessArtifactLimit})
		if err != nil {
			return edge.OperationResult{}, projectBrowserHarnessSafeCode(err)
		}
		base.BrowserHarnessRunID = chunk.RunID
		base.BrowserHarnessArtifactPath = chunk.Path
		base.BrowserHarnessArtifactMediaType = chunk.MediaType
		base.BrowserHarnessArtifactBytes = chunk.Bytes
		base.BrowserHarnessArtifactSHA256 = chunk.SHA256
		base.BrowserHarnessArtifactOffset = chunk.Offset
		base.BrowserHarnessArtifactNext = chunk.Next
		base.BrowserHarnessArtifactEOF = chunk.EOF
		base.BrowserHarnessArtifactDataBase64 = chunk.DataBase64
		return base, ""
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

func projectBrowserHarnessSnapshotResult(base edge.OperationResult, snapshot edgeclient.ProjectBrowserHarnessSnapshot) edge.OperationResult {
	base.BrowserHarnessRunID = snapshot.RunID
	base.BrowserHarnessState = snapshot.State
	base.BrowserHarnessProfile = snapshot.Profile
	base.BrowserHarnessCreatedAt = snapshot.CreatedAt.UTC().Format(time.RFC3339Nano)
	base.BrowserHarnessUpdatedAt = snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if !snapshot.StartedAt.IsZero() {
		base.BrowserHarnessStartedAt = snapshot.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !snapshot.FinishedAt.IsZero() {
		base.BrowserHarnessFinishedAt = snapshot.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	base.BrowserHarnessExitKnown = snapshot.ExitKnown
	base.BrowserHarnessExitCode = snapshot.ExitCode
	base.BrowserHarnessTimeoutSeconds = snapshot.TimeoutSeconds
	base.BrowserHarnessStorageMiB = snapshot.StorageMiB
	base.BrowserHarnessStdout = snapshot.Stdout
	base.BrowserHarnessStderr = snapshot.Stderr
	base.BrowserHarnessStdoutNext = snapshot.StdoutNext
	base.BrowserHarnessStderrNext = snapshot.StderrNext
	base.BrowserHarnessStdoutEOF = snapshot.StdoutEOF
	base.BrowserHarnessStderrEOF = snapshot.StderrEOF
	base.BrowserHarnessStdoutTruncated = snapshot.StdoutTruncated
	base.BrowserHarnessStderrTruncated = snapshot.StderrTruncated
	base.BrowserHarnessArtifactCount = snapshot.ArtifactCount
	base.BrowserHarnessArtifactBytes = snapshot.ArtifactBytes
	return base
}

func projectBrowserHarnessSummaryResult(run edgeclient.ProjectBrowserHarnessSummary) edge.BrowserHarnessSummary {
	result := edge.BrowserHarnessSummary{RunID: run.RunID, State: run.State, Profile: run.Profile, CreatedAt: run.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: run.UpdatedAt.UTC().Format(time.RFC3339Nano), ExitKnown: run.ExitKnown, ExitCode: run.ExitCode, TimeoutSeconds: run.TimeoutSeconds, StorageMiB: run.StorageMiB}
	if !run.StartedAt.IsZero() {
		result.StartedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !run.FinishedAt.IsZero() {
		result.FinishedAt = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func projectBrowserHarnessSafeCode(err error) string {
	switch {
	case errors.Is(err, edgeclient.ErrProjectToolboxNotFound):
		return "project_browser_harness_not_found"
	case errors.Is(err, edgeclient.ErrProjectToolboxNotOwned), errors.Is(err, edgeclient.ErrProjectToolboxUnsafeState):
		return "project_browser_harness_invalid"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	default:
		return "project_browser_harness_failed"
	}
}
