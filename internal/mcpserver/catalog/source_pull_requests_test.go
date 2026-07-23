package catalog

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type fakeSourcePullRequestService struct {
	calls []string
}

func (f *fakeSourcePullRequestService) SourcePullRequestCreatePreview(repo, head, base, title, description string) (string, error) {
	f.calls = append(f.calls, "create-preview:"+repo+":"+head+":"+base+":"+title+":"+description)
	return "create-preview-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "create:"+planID)
	if approve {
		f.calls = append(f.calls, "create-approved")
	}
	return "create-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestStatus(repo string, number int) (string, error) {
	f.calls = append(f.calls, "status:"+repo)
	if number == 17 {
		f.calls = append(f.calls, "status-number")
	}
	return "status-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestFailureDiagnostics(repo string, number int, workflowName, jobName string, maxLines int) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("diagnostics:%s:%d:%s:%s:%d", repo, number, workflowName, jobName, maxLines))
	return "diagnostics-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestJobLog(repo string, number int, workflowName, jobName string, offsetBytes, maxBytes int) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("job-log:%s:%d:%s:%s:%d:%d", repo, number, workflowName, jobName, offsetBytes, maxBytes))
	return "job-log-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestMergePreview(repo string, number int) (string, error) {
	f.calls = append(f.calls, "merge-preview:"+repo)
	if number == 17 {
		f.calls = append(f.calls, "merge-number")
	}
	return "merge-preview-result", nil
}

func (f *fakeSourcePullRequestService) SourcePullRequestMerge(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "merge:"+planID)
	if approve {
		f.calls = append(f.calls, "merge-approved")
	}
	return "merge-result", nil
}

func (f *fakeSourcePullRequestService) SourceDefaultBranchUpdatePreview(repo, branch string) (string, error) {
	f.calls = append(f.calls, "default-preview:"+repo+":"+branch)
	return "default-preview-result", nil
}

func (f *fakeSourcePullRequestService) SourceDefaultBranchUpdate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "default:"+planID)
	if approve {
		f.calls = append(f.calls, "default-approved")
	}
	return "default-result", nil
}

func TestRegisterSourcePullRequestsRoutesEveryHandler(t *testing.T) {
	service := &fakeSourcePullRequestService{}
	byName := map[string]Tool{}
	RegisterSourcePullRequests(func(tool Tool) {
		byName[tool.Name] = tool
	}, service)

	wantNames := []string{
		"source_pull_request_create_preview",
		"source_pull_request_create",
		"source_pull_request_status",
		"source_pull_request_failure_diagnostics",
		"source_pull_request_job_log",
		"source_pull_request_merge_preview",
		"source_pull_request_merge",
		"source_default_branch_update_preview",
		"source_default_branch_update",
	}
	gotNames := make([]string, 0, len(byName))
	for _, name := range wantNames {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		if tool.Version != "1" || tool.Description == "" || tool.InputSchema == nil {
			t.Fatalf("invalid contract for %s", name)
		}
		gotNames = append(gotNames, tool.Name)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{"source_pull_request_create_preview", `{"repo":"mcp-devbox","head":"feature","base":"main","title":"Title","description":"Body"}`, "create-preview-result"},
		{"source_pull_request_create", `{"plan_id":"create-plan","approve":true}`, "create-result"},
		{"source_pull_request_status", `{"repo":"mcp-devbox","number":17}`, "status-result"},
		{"source_pull_request_failure_diagnostics", `{"repo":"mcp-devbox","number":17,"workflow_name":"P15","job_name":"Package","max_lines":200}`, "diagnostics-result"},
		{"source_pull_request_job_log", `{"repo":"mcp-devbox","number":17,"workflow_name":"P15","job_name":"Package","offset_bytes":1024,"max_bytes":4096}`, "job-log-result"},
		{"source_pull_request_merge_preview", `{"repo":"mcp-devbox","number":17}`, "merge-preview-result"},
		{"source_pull_request_merge", `{"plan_id":"merge-plan","approve":true}`, "merge-result"},
		{"source_default_branch_update_preview", `{"repo":"mcp-devbox","branch":"main"}`, "default-preview-result"},
		{"source_default_branch_update", `{"plan_id":"default-plan","approve":true}`, "default-result"},
	}
	for _, test := range cases {
		got, err := byName[test.name].Handler(json.RawMessage(test.body))
		if err != nil || got != test.want {
			t.Fatalf("%s result=%q err=%v", test.name, got, err)
		}
		if _, err := byName[test.name].Handler(json.RawMessage(`{"broken"`)); err == nil {
			t.Fatalf("%s accepted malformed JSON", test.name)
		}
	}

	wantCalls := []string{
		"create-preview:mcp-devbox:feature:main:Title:Body",
		"create:create-plan", "create-approved",
		"status:mcp-devbox", "status-number",
		"diagnostics:mcp-devbox:17:P15:Package:200",
		"job-log:mcp-devbox:17:P15:Package:1024:4096",
		"merge-preview:mcp-devbox", "merge-number",
		"merge:merge-plan", "merge-approved",
		"default-preview:mcp-devbox:main",
		"default:default-plan", "default-approved",
	}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
