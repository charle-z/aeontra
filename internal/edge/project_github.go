package edge

import (
	"errors"
	"strings"
)

func normalizeProjectGitHubRequest(request OperationRequest) (OperationRequest, error) {
	if !emptyProjectExecRequestFields(request) || !emptyProjectProcessRequestFields(request) || request.GitPlanID != "" ||
		request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request) {
		return OperationRequest{}, errors.New("project GitHub request is invalid")
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	if !validProjectOperationRequestCommon(request) || request.Repository != "" || request.IdempotencyKey != "" {
		return OperationRequest{}, errors.New("project GitHub request is invalid")
	}
	return request, nil
}

func hasProjectGitHubResult(result OperationResult) bool {
	return result.GitHubConfigured || result.GitHubVisibility != "" || result.GitHubDefaultBranch != "" || result.GitHubArchived ||
		result.GitHubMetadataRead || result.GitHubContentsRead || result.GitHubContentsWrite || result.GitHubPullRequestsRead ||
		result.GitHubActionsRead || result.GitHubAdministration || len(result.GitHubPermissionIssues) != 0
}

func validProjectGitHubResult(result OperationResult) bool {
	if !result.GitHubConfigured || !result.GitHubMetadataRead ||
		(result.GitHubVisibility != "private" && result.GitHubVisibility != "public" && result.GitHubVisibility != "internal") ||
		!projectSnapshotBranchPattern.MatchString(result.GitHubDefaultBranch) || len(result.GitHubPermissionIssues) > 4 {
		return false
	}
	issues := map[string]bool{}
	for _, issue := range result.GitHubPermissionIssues {
		if issues[issue] {
			return false
		}
		issues[issue] = true
	}
	expected := map[string]bool{
		"contents_read_denied":      !result.GitHubContentsRead,
		"contents_write_denied":     !result.GitHubContentsWrite,
		"pull_requests_read_denied": !result.GitHubPullRequestsRead,
		"actions_read_denied":       !result.GitHubActionsRead,
	}
	if len(issues) != 0 {
		for issue := range issues {
			if _, ok := expected[issue]; !ok {
				return false
			}
		}
	}
	for issue, required := range expected {
		if issues[issue] != required {
			return false
		}
	}
	metadata := result
	metadata.GitHubConfigured = false
	metadata.GitHubVisibility = ""
	metadata.GitHubDefaultBranch = ""
	metadata.GitHubArchived = false
	metadata.GitHubMetadataRead = false
	metadata.GitHubContentsRead = false
	metadata.GitHubContentsWrite = false
	metadata.GitHubPullRequestsRead = false
	metadata.GitHubActionsRead = false
	metadata.GitHubAdministration = false
	metadata.GitHubPermissionIssues = nil
	return validProjectOperationResult(metadata)
}
