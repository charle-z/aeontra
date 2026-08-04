//go:build !windows

package main

import (
	"context"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeProjectBrowser(ctx context.Context, stateRoot string, manager *edgeclient.ProjectBrowserManager, operation edge.Operation) (edge.OperationResult, string) {
	if manager == nil {
		return edge.OperationResult{}, "project_browser_unavailable"
	}
	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, "project_browser_failed"
	}
	base := edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}
	switch operation.Kind {
	case edge.OperationProjectBrowserCreate:
		snapshot, _, err := manager.Create(ctx, edgeclient.ProjectBrowserCreateRequest{IdempotencyKey: operation.Request.IdempotencyKey, Resolution: resolved, NetworkScope: operation.Request.BrowserNetworkScope, InitialURL: operation.Request.BrowserInitialURL, ViewportWidth: operation.Request.BrowserViewportWidth, ViewportHeight: operation.Request.BrowserViewportHeight, IgnoreHTTPSErrors: operation.Request.BrowserIgnoreHTTPSErrors})
		if err != nil {
			return edge.OperationResult{}, "project_browser_create_failed"
		}
		return browserSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserStatus:
		snapshot, err := manager.Status(edgeclient.ProjectBrowserReadRequest{Resolution: resolved, SessionID: operation.Request.BrowserSessionID})
		if err != nil {
			return edge.OperationResult{}, "project_browser_status_failed"
		}
		return browserSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserList:
		sessions, err := manager.List(edgeclient.ProjectBrowserListRequest{Resolution: resolved, Limit: edge.MaxBrowserSessions})
		if err != nil {
			return edge.OperationResult{}, "project_browser_list_failed"
		}
		base.BrowserListComplete = true
		base.BrowserSessions = make([]edge.BrowserSessionSummary, 0, len(sessions))
		for _, s := range sessions {
			base.BrowserSessions = append(base.BrowserSessions, browserSessionSummary(s))
		}
		return base, ""
	case edge.OperationProjectBrowserRun:
		snapshot, err := manager.Run(ctx, edgeclient.ProjectBrowserRunRequest{OperationID: operation.ID, IdempotencyKey: operation.Request.IdempotencyKey, Resolution: resolved, SessionID: operation.Request.BrowserSessionID, Steps: operation.Request.BrowserSteps, Capture: operation.Request.BrowserCapture, FullPage: operation.Request.BrowserFullPage, TimeoutSeconds: operation.Request.BrowserTimeoutSeconds})
		if err != nil {
			return edge.OperationResult{}, "project_browser_run_failed"
		}
		result := browserSnapshotResult(base, snapshot)
		result.BrowserText = snapshot.Text
		result.BrowserTextTruncated = snapshot.TextTruncated
		result.BrowserArtifactID = snapshot.ArtifactID
		result.BrowserArtifactMediaType = snapshot.ArtifactMediaType
		result.BrowserArtifactBytes = snapshot.ArtifactBytes
		result.BrowserArtifactSHA256 = snapshot.ArtifactSHA256
		return result, ""
	case edge.OperationProjectBrowserArtifactRead:
		chunk, err := manager.ReadArtifact(edgeclient.ProjectBrowserArtifactReadRequest{Resolution: resolved, SessionID: operation.Request.BrowserSessionID, ArtifactID: operation.Request.BrowserArtifactID, Offset: operation.Request.BrowserArtifactOffset, Limit: operation.Request.BrowserArtifactLimit})
		if err != nil {
			return edge.OperationResult{}, "project_browser_artifact_failed"
		}
		base.BrowserSessionID = chunk.SessionID
		base.BrowserArtifactID = chunk.ArtifactID
		base.BrowserArtifactMediaType = chunk.MediaType
		base.BrowserArtifactBytes = chunk.Bytes
		base.BrowserArtifactSHA256 = chunk.SHA256
		base.BrowserArtifactOffset = chunk.Offset
		base.BrowserArtifactNext = chunk.Next
		base.BrowserArtifactEOF = chunk.EOF
		base.BrowserArtifactDataBase64 = chunk.DataBase64
		return base, ""
	case edge.OperationProjectBrowserClose:
		snapshot, err := manager.CloseSession(edgeclient.ProjectBrowserCloseRequest{Resolution: resolved, SessionID: operation.Request.BrowserSessionID})
		if err != nil {
			return edge.OperationResult{}, "project_browser_close_failed"
		}
		return browserSnapshotResult(base, snapshot), ""
	case edge.OperationProjectBrowserCleanup:
		cleaned, err := manager.Cleanup(edgeclient.ProjectBrowserCleanupRequest{Resolution: resolved, SessionID: operation.Request.BrowserSessionID})
		if err != nil {
			return edge.OperationResult{}, "project_browser_cleanup_failed"
		}
		base.BrowserCleanupCompleted = true
		base.BrowserCleanupRemoved = cleaned.Removed
		base.BrowserCleanupArtifacts = cleaned.Artifacts
		return base, ""
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}

func browserSnapshotResult(base edge.OperationResult, s edgeclient.ProjectBrowserSnapshot) edge.OperationResult {
	base.BrowserSessionID = s.SessionID
	base.BrowserState = s.State
	base.BrowserNetworkScope = s.NetworkScope
	base.BrowserSafeURL = s.SafeURL
	base.BrowserTitle = s.Title
	base.BrowserRevision = s.Revision
	base.BrowserCreatedAt = s.CreatedAt
	base.BrowserUpdatedAt = s.UpdatedAt
	return base
}
func browserSessionSummary(s edgeclient.ProjectBrowserSnapshot) edge.BrowserSessionSummary {
	return edge.BrowserSessionSummary{SessionID: s.SessionID, State: s.State, NetworkScope: s.NetworkScope, SafeURL: s.SafeURL, Title: s.Title, Revision: s.Revision, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}
