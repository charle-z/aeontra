package main

import (
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectProcessListResultUsesAttestedBindingForLegacyRecord(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo", ClaimGeneration: 7},
		TargetAlias: "parrot", Workspace: edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
		CheckoutState: edgeclient.ProjectCheckoutRegistered, RegisteredOnly: true,
	}
	started := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	legacy := edgeclient.ProjectProcessSnapshot{
		ProcessID: "pr_0123456789abcdef0123456789abcdef", WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
		ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode), ProjectState: "ready",
		State: edgeclient.ProjectProcessStopped, StartedAt: started, FinishedAt: started.Add(time.Minute), Reason: "process_stopped_while_offline",
	}
	result := projectProcessListResult(resolved, []edgeclient.ProjectProcessSnapshot{legacy})
	if result.ProjectOwner != resolved.Project.Owner || result.ProjectRepository != resolved.Project.Repository || result.ProjectState != "ready" {
		t.Fatalf("legacy process list metadata owner=%q repository=%q state=%q", result.ProjectOwner, result.ProjectRepository, result.ProjectState)
	}
	if len(result.BackgroundProcesses) != 1 || result.BackgroundProcesses[0].ProcessID != legacy.ProcessID {
		t.Fatalf("legacy process was omitted: %+v", result.BackgroundProcesses)
	}
}

func TestProjectProcessListResultDoesNotRebindDifferentWorkspace(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"},
		TargetAlias: "parrot", Workspace: edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef"},
	}
	legacy := edgeclient.ProjectProcessSnapshot{WorkspaceID: "ws_ffffffffffffffffffffffffffffffff", ProjectAlias: "project", TargetAlias: "parrot"}
	result := projectProcessListResult(resolved, []edgeclient.ProjectProcessSnapshot{legacy})
	if result.ProjectOwner != "" || result.ProjectRepository != "" {
		t.Fatalf("foreign workspace was attributed to current project: owner=%q repository=%q", result.ProjectOwner, result.ProjectRepository)
	}
}

func TestProjectProcessListResultDoesNotReplaceRecordedIdentity(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "new-repo"},
		TargetAlias: "parrot", Workspace: edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef"},
		RegisteredOnly: true,
	}
	for _, snapshot := range []edgeclient.ProjectProcessSnapshot{
		{WorkspaceID: resolved.Workspace.ID, ProjectAlias: "project", TargetAlias: "parrot", ProjectOwner: "charle-z", ProjectRepository: "old-repo", ProjectClaimGeneration: 1},
		{WorkspaceID: resolved.Workspace.ID, ProjectAlias: "project", TargetAlias: "parrot", ProjectOwner: "charle-z", ProjectRepository: "", ProjectClaimGeneration: 1},
	} {
		result := projectProcessListResult(resolved, []edgeclient.ProjectProcessSnapshot{snapshot})
		if result.ProjectOwner != snapshot.ProjectOwner || result.ProjectRepository != snapshot.ProjectRepository {
			t.Fatalf("recorded identity was replaced: owner=%q repository=%q", result.ProjectOwner, result.ProjectRepository)
		}
	}
}
