package edge

import (
	"strings"
	"testing"
	"time"
)

func projectWorktreeTestRequest(kind OperationKind) OperationRequest {
	request := OperationRequest{Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell"}
	switch kind {
	case OperationProjectWorktreeTestProfile:
		request.IdempotencyKey = "task-profile-0001"
		request.TestProfileID = "3"
	case OperationProjectWorktreeTestStart:
		request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence = "wt_0123456789abcdef0123456789abcdef", "wj_0123456789abcdef0123456789abcdef", "wl_0123456789abcdef0123456789abcdef", 7
		request.TestProfileID, request.TestProfileDigest, request.IdempotencyKey = "linux-workcell", "sha256:"+strings.Repeat("a", 64), "task-start-0001"
	case OperationProjectWorktreeTestStatus:
		request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence = "wt_0123456789abcdef0123456789abcdef", "wj_0123456789abcdef0123456789abcdef", "wl_0123456789abcdef0123456789abcdef", 7
		request.BackgroundProcessID, request.TestProfileID, request.TestProfileDigest = "pr_0123456789abcdef0123456789abcdef", "linux-workcell", "sha256:"+strings.Repeat("a", 64)
	case OperationProjectWorktreeTestStop:
		request.WorktreeID, request.WorkJobID, request.WorkLeaseID, request.WorkFence = "wt_0123456789abcdef0123456789abcdef", "wj_0123456789abcdef0123456789abcdef", "wl_0123456789abcdef0123456789abcdef", 7
		request.BackgroundProcessID, request.TestProfileID, request.TestProfileDigest, request.IdempotencyKey = "pr_0123456789abcdef0123456789abcdef", "linux-workcell", "sha256:"+strings.Repeat("a", 64), "task-stop-0001"
	}
	return request
}

func TestProjectWorktreeTestRequestsAreClosedAndFenced(t *testing.T) {
	for _, kind := range []OperationKind{OperationProjectWorktreeTestProfile, OperationProjectWorktreeTestStart, OperationProjectWorktreeTestStatus, OperationProjectWorktreeTestStop} {
		request := projectWorktreeTestRequest(kind)
		normalized, err := validateOperationRequestWithProjectExec(kind, request)
		if err != nil {
			t.Fatalf("kind %q rejected valid request: %v", kind, err)
		}
		if normalized.Alias != "project" || normalized.TargetAlias != "parrot" {
			t.Fatalf("kind %q did not normalize project identity: %+v", kind, normalized)
		}
		if kind == OperationProjectWorktreeTestStatus && normalized.OutputLimit != MaxProjectProcessReadBytes {
			t.Fatalf("status default output limit = %d", normalized.OutputLimit)
		}
		for name, mutate := range map[string]func(*OperationRequest){
			"argv":        func(value *OperationRequest) { value.Argv = []string{"sh", "-c", "caller"} },
			"cwd":         func(value *OperationRequest) { value.CWD = "../../outside" },
			"environment": func(value *OperationRequest) { value.Environment = map[string]string{"PATH": "/tmp"} },
		} {
			invalid := request
			mutate(&invalid)
			if _, err := validateOperationRequestWithProjectExec(kind, invalid); err == nil {
				t.Errorf("kind %q accepted caller-controlled %s", kind, name)
			}
		}
	}

	missingFence := projectWorktreeTestRequest(OperationProjectWorktreeTestStart)
	missingFence.WorkFence = 0
	if _, err := validateOperationRequestWithProjectExec(OperationProjectWorktreeTestStart, missingFence); err == nil {
		t.Fatal("start accepted missing fence")
	}
	missingProfile := projectWorktreeTestRequest(OperationProjectWorktreeTestStatus)
	missingProfile.TestProfileDigest = ""
	if _, err := validateOperationRequestWithProjectExec(OperationProjectWorktreeTestStatus, missingProfile); err == nil {
		t.Fatal("status accepted missing pinned profile digest")
	}
}

func validProjectWorktreeTestOperationResult() OperationResult {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return OperationResult{
		WorkspaceID: "ws_abcdefabcdefabcdefabcdefabcdefab",
		WorktreeID:  "wt_0123456789abcdef0123456789abcdef", WorktreeState: "ready", WorktreeRole: "writer",
		WorktreeBaseCommit: strings.Repeat("a", 40), WorktreeHeadCommit: strings.Repeat("b", 40), WorktreeBranch: "codex/worktree-0123456789abcdef0123456789abcdef",
		WorkJobID: "wj_0123456789abcdef0123456789abcdef", WorkLeaseID: "wl_0123456789abcdef0123456789abcdef", WorkFence: 7,
		TestProfileID: "linux-workcell", TestProfileDigest: "sha256:" + strings.Repeat("c", 64), TestTimeoutSeconds: 300,
		ContentDigest: "sha256:" + strings.Repeat("d", 64), BackgroundProcessID: "pr_0123456789abcdef0123456789abcdef",
		BackgroundProcessState: "running", BackgroundStartedAt: now,
	}
}

func TestProjectWorktreeTestResultsAreTypedBoundedAndPathFree(t *testing.T) {
	profile := OperationResult{TestProfileID: "3", TestProfileDigest: "sha256:" + strings.Repeat("e", 64), TestTimeoutSeconds: 30}
	if !validOperationCompletionForKind(OperationProjectWorktreeTestProfile, profile, "") {
		t.Fatal("valid profile metadata rejected")
	}
	profile.WorkspaceID = "ws_abcdefabcdefabcdefabcdefabcdefab"
	if validOperationCompletionForKind(OperationProjectWorktreeTestProfile, profile, "") {
		t.Fatal("profile metadata leaked unrelated workspace identity")
	}

	start := validProjectWorktreeTestOperationResult()
	if !validOperationCompletionForKind(OperationProjectWorktreeTestStart, start, "") {
		t.Fatal("valid start evidence rejected")
	}
	for name, mutate := range map[string]func(*OperationResult){
		"base commit": func(value *OperationResult) { value.WorktreeBaseCommit = "../../host" },
		"digest":      func(value *OperationResult) { value.ContentDigest = "sha256:short" },
		"path reason": func(value *OperationResult) { value.BackgroundReason = `C:\Users\worker` },
	} {
		invalid := start
		mutate(&invalid)
		if validOperationCompletionForKind(OperationProjectWorktreeTestStart, invalid, "") {
			t.Errorf("accepted invalid %s", name)
		}
	}

	status := start
	status.TestStale, status.TestStaleReason = true, "content_changed"
	if !validOperationCompletionForKind(OperationProjectWorktreeTestStatus, status, "") {
		t.Fatal("valid stale status rejected")
	}
	status.TestStaleReason = `C:\Users\worker`
	if validOperationCompletionForKind(OperationProjectWorktreeTestStatus, status, "") {
		t.Fatal("stale status accepted path-bearing reason")
	}

	stopped := start
	stopped.BackgroundProcessState, stopped.BackgroundFinishedAt = "stopped", time.Now().UTC().Format(time.RFC3339Nano)
	stopped.BackgroundTerminalSignal = "terminate"
	if !validOperationCompletionForKind(OperationProjectWorktreeTestStop, stopped, "") {
		t.Fatal("valid stop evidence rejected")
	}
}
