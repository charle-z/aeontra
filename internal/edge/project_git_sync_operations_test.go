package edge

import "testing"

func TestProjectGitSyncRequestsAreClosedAndDurable(t *testing.T) {
	base := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}
	for _, kind := range []OperationKind{OperationProjectGitStatus, OperationProjectGitFetch, OperationProjectGitFastForwardPreview} {
		request := base
		if kind != OperationProjectGitStatus {
			request.IdempotencyKey = "sync-1"
		}
		if _, err := validateOperationRequestWithProjectExec(kind, request); err != nil {
			t.Fatalf("%s rejected: %v", kind, err)
		}
	}
	execute := base
	execute.IdempotencyKey = "sync-2"
	execute.GitPlanID = "gp_0123456789abcdef0123456789abcdef"
	if _, err := validateOperationRequestWithProjectExec(OperationProjectGitFastForward, execute); err != nil {
		t.Fatal(err)
	}

	invalid := []OperationRequest{
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", Repository: "other"},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", GitPlanID: "gp_bad"},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "sync-1", Argv: []string{"git"}},
	}
	for _, request := range invalid {
		if _, err := validateOperationRequestWithProjectExec(OperationProjectGitFastForward, request); err == nil {
			t.Fatalf("accepted invalid request: %+v", request)
		}
	}
}

func TestProjectGitSyncCompletionRejectsMixedOrLeakyResults(t *testing.T) {
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		GitBranch: "main", GitHead: "0123456789abcdef0123456789abcdef01234567",
		GitRemoteHead: "1123456789abcdef0123456789abcdef01234567", GitBehind: 1, GitClean: true,
	}
	if !validOperationCompletionForKind(OperationProjectGitStatus, result, "") {
		t.Fatal("valid Git status result rejected")
	}
	if validOperationCompletionForKind(OperationProjectGitFetch, result, "") {
		t.Fatal("fetch accepted a result without fetched remote tracking")
	}
	detached := result
	detached.GitBranch, detached.GitRemoteHead, detached.GitBehind = "", "", 0
	detached.GitDetached = true
	if !validOperationCompletionForKind(OperationProjectGitStatus, detached, "") {
		t.Fatal("bounded detached status was rejected")
	}
	result.ExecStdout = "leak"
	if validOperationCompletionForKind(OperationProjectGitStatus, result, "") {
		t.Fatal("mixed execution result accepted")
	}
}
