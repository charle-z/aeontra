//go:build windows

package main

import (
	"context"
	"errors"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func executeWindowsProjectGitHubStatus(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, "project_github_status_failed"
	}
	result, err := collectWindowsProjectGitHubStatus(ctx, resolved, credential, edgeclient.NewGitHubCommandRunner(stateRoot, ""))
	if err != nil {
		return edge.OperationResult{}, "project_github_status_failed"
	}
	return result, ""
}

func collectWindowsProjectGitHubStatus(ctx context.Context, resolved edgeclient.ProjectResolution, credential edgeclient.GitHubCredential, runner edgeclient.GitHubCommandRunner) (edge.OperationResult, error) {
	if resolved.Workspace.Profile != edgeclient.WorkspaceProfileWindowsWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev ||
		resolved.Project.Owner != credential.Owner || runner == nil {
		return edge.OperationResult{}, errors.New("project GitHub authority is unavailable")
	}
	status, err := edgeclient.NewGitHubBrokerClient(edgeclient.GitHubBrokerClientConfig{Credential: credential, Runner: runner}).Status(ctx, resolved.Project.Repository)
	if err != nil {
		return edge.OperationResult{}, err
	}
	result := windowsProjectMetadata(resolved)
	result.GitHubConfigured = status.Configured
	result.GitHubVisibility = status.Visibility
	result.GitHubDefaultBranch = status.DefaultBranch
	result.GitHubArchived = status.Archived
	result.GitHubMetadataRead = status.Capabilities.MetadataRead
	result.GitHubContentsRead = status.Capabilities.ContentsRead
	result.GitHubContentsWrite = status.Capabilities.ContentsWrite
	result.GitHubPullRequestsRead = status.Capabilities.PullRequestsRead
	result.GitHubActionsRead = status.Capabilities.ActionsRead
	result.GitHubAdministration = status.Capabilities.Administration
	result.GitHubPermissionIssues = append([]string(nil), status.PermissionIssues...)
	return result, nil
}

func windowsProjectMetadata(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{
		WorkspaceID:       resolved.Workspace.ID,
		ProjectAlias:      resolved.Project.Alias,
		ProjectOwner:      resolved.Project.Owner,
		ProjectRepository: resolved.Project.Repository,
		ProjectTarget:     resolved.TargetAlias,
		ProjectState:      resolved.SafeState(),
		ProjectProfile:    string(resolved.Workspace.Profile),
		ProjectMode:       string(resolved.Workspace.Mode),
	}
}
