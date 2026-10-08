package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectGitHubStatusParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
}

type projectGitHubCapabilitiesView struct {
	MetadataRead     bool `json:"metadata_read"`
	ContentsRead     bool `json:"contents_read"`
	ContentsWrite    bool `json:"contents_write"`
	PullRequestsRead bool `json:"pull_requests_read"`
	ActionsRead      bool `json:"actions_read"`
	Administration   bool `json:"administration"`
}

type projectGitHubStatusPublicView struct {
	OperationID      string                        `json:"operation_id"`
	OperationState   edge.OperationState           `json:"operation_state"`
	Alias            string                        `json:"alias"`
	Repository       string                        `json:"repository,omitempty"`
	Target           string                        `json:"target"`
	Configured       bool                          `json:"configured"`
	Visibility       string                        `json:"visibility,omitempty"`
	DefaultBranch    string                        `json:"default_branch,omitempty"`
	Archived         bool                          `json:"archived"`
	Capabilities     projectGitHubCapabilitiesView `json:"capabilities"`
	PermissionIssues []string                      `json:"permission_issues,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
}

func (s *Server) addProjectGitHubTools(projectSchema map[string]any) {
	s.addDirectTool(toolDef{
		Name:        "project_github_status",
		Description: "Verify the private Edge GitHub authority against the repository already bound to a development project. Returns only owner-bound repository metadata and safe contents, pull-request and Actions permission diagnostics; the credential, URL, paths and headers never leave the Edge.",
		InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}, []string{"alias", "target"}),
		Version:     "1",
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true},
	}, func(raw json.RawMessage) (string, error) { return s.handleProjectGitHubStatus(raw) })
}

func (s *Server) handleProjectGitHubStatus(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectGitHubStatusParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	request := edge.OperationRequest{Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell"}
	op, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectGitHubStatus, request)
	if err == nil {
		op, err = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
	}
	view := projectGitHubStatusPublicView{OperationID: op.ID, OperationState: op.State, Alias: params.Alias, Target: params.Target}
	if op.State == edge.OperationSucceeded {
		result := op.Result
		view.Alias = result.ProjectAlias
		view.Repository = result.ProjectOwner + "/" + result.ProjectRepository
		view.Target = result.ProjectTarget
		view.Configured = result.GitHubConfigured
		view.Visibility = result.GitHubVisibility
		view.DefaultBranch = result.GitHubDefaultBranch
		view.Archived = result.GitHubArchived
		view.Capabilities = projectGitHubCapabilitiesView{
			MetadataRead: result.GitHubMetadataRead, ContentsRead: result.GitHubContentsRead, ContentsWrite: result.GitHubContentsWrite,
			PullRequestsRead: result.GitHubPullRequestsRead, ActionsRead: result.GitHubActionsRead, Administration: result.GitHubAdministration,
		}
		view.PermissionIssues = append([]string(nil), result.GitHubPermissionIssues...)
	} else if op.State == edge.OperationFailed || op.State == edge.OperationCancelled {
		view.Reason = op.SafeCode
	}
	return marshalToolValue(view, err)
}
