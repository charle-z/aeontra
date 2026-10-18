package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSourcePullRequestJobLogUsesLatestWorkflowAttemptOnly(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/demo/pulls/48":
			writeDiagnosticsPull(w)
		case r.URL.Path == "/repos/acme/demo/actions/runs":
			_ = json.NewEncoder(w).Encode(githubActionsRunsResponse{TotalCount: 2, WorkflowRuns: []githubActionsRun{
				{ID: 10, WorkflowID: 100, RunAttempt: 1, Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: diagnosticsHeadSHA},
				{ID: 11, WorkflowID: 100, RunAttempt: 2, Name: "CI", Status: "completed", Conclusion: "failure", HeadSHA: diagnosticsHeadSHA},
			}})
		case r.URL.Path == "/repos/acme/demo/actions/runs/10/jobs":
			t.Fatal("older workflow attempt was queried")
		case r.URL.Path == "/repos/acme/demo/actions/runs/11/jobs":
			_ = json.NewEncoder(w).Encode(githubActionsJobsResponse{TotalCount: 1, Jobs: []githubActionsJob{{ID: 21, Name: "Package", Status: "completed", Conclusion: "failure"}}})
		case r.URL.Path == "/repos/acme/demo/actions/jobs/21/logs":
			w.Header().Set("Location", server.URL+"/signed/latest.log")
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/signed/latest.log":
			_, _ = fmt.Fprintln(w, "latest attempt diagnostic")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "configured-token", "acme", "org", "private"))
	output, err := svc.SourcePullRequestJobLog("demo", 48, "CI", "Package", 0, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "run_attempt: 2") || !strings.Contains(output, "latest attempt diagnostic") {
		t.Fatalf("latest attempt output=%s", output)
	}
}
