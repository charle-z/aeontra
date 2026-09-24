package main

import (
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func projectProcessListResult(resolved edgeclient.ProjectResolution, items []edgeclient.ProjectProcessSnapshot) edge.OperationResult {
	result := projectProcessBaseResult(resolved)
	if len(items) > 0 {
		result = projectProcessBaseResult(durableProjectProcessResolutionFromSnapshot(items[0]))
		// Journals created before process binding metadata was added retain an
		// empty owner/repository. ResolveRegistered has already attested the
		// current workspace, and List scopes records to that exact workspace.
		// Restore only current response metadata; the original owner remains
		// unknown and the durable process record is never rewritten.
		first := items[0]
		if resolved.RegisteredOnly && first.ProjectOwner == "" && first.ProjectRepository == "" && first.ProjectClaimGeneration == 0 &&
			first.WorkspaceID == resolved.Workspace.ID && first.ProjectAlias == resolved.Project.Alias && first.TargetAlias == resolved.TargetAlias &&
			first.ProjectProfile == string(resolved.Workspace.Profile) && first.ProjectMode == string(resolved.Workspace.Mode) {
			result.ProjectOwner = resolved.Project.Owner
			result.ProjectRepository = resolved.Project.Repository
		}
	}
	result.BackgroundProcesses = make([]edge.BackgroundProcessSummary, 0, len(items))
	for _, item := range items {
		result.BackgroundProcesses = append(result.BackgroundProcesses, projectProcessSummary(item))
	}
	return result
}
