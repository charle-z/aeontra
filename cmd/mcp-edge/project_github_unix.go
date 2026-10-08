//go:build !windows

package main

import (
	"context"
	"errors"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func collectProjectGitHubStatus(ctx context.Context, resolved edgeclient.ProjectResolution, credential edgeclient.GitHubCredential, runner edgeclient.GitHubCommandRunner) (edge.OperationResult, error) {
	if resolved.Workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev ||
		resolved.Project.Owner != credential.Owner || runner == nil {
		return edge.OperationResult{}, errors.New("project GitHub authority is unavailable")
	}
	status, err := edgeclient.NewGitHubBrokerClient(edgeclient.GitHubBrokerClientConfig{Credential: credential, Runner: runner}).Status(ctx, resolved.Project.Repository)
	if err != nil {
		return edge.OperationResult{}, err
	}
	result := projectGitMetadata(resolved)
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
