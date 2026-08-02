package edge

import "testing"

func TestProjectGitHubStatusRequestAndResultAreClosed(t *testing.T) {
	request := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectGitHubStatus, request); err != nil {
		t.Fatal(err)
	}
	request.Repository = "other"
	if _, err := validateOperationRequestWithProjectExec(OperationProjectGitHubStatus, request); err == nil {
		t.Fatal("caller-selected repository accepted")
	}

	result := OperationResult{
		WorkspaceID: "ws_0123456789abcdef0123456789abcdef", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		GitHubConfigured: true, GitHubVisibility: "private", GitHubDefaultBranch: "main",
		GitHubMetadataRead: true, GitHubContentsRead: true, GitHubContentsWrite: true, GitHubPullRequestsRead: true, GitHubActionsRead: true,
	}
	if !validOperationCompletionForKind(OperationProjectGitHubStatus, result, "") {
		t.Fatal("valid GitHub status result rejected")
	}
	result.GitHubContentsRead = false
	result.GitHubContentsWrite = false
	result.GitHubPermissionIssues = []string{"contents_read_denied", "contents_write_denied"}
	if !validOperationCompletionForKind(OperationProjectGitHubStatus, result, "") {
		t.Fatal("bounded insufficient-permission status rejected")
	}
	result.GitHubPermissionIssues = []string{"credential=github_pat_secret"}
	if validOperationCompletionForKind(OperationProjectGitHubStatus, result, "") {
		t.Fatal("unbounded permission diagnostic accepted")
	}
}
