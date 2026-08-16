package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

const evidenceTestSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type evidenceTestRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	RunAttempt int    `json:"run_attempt"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url,omitempty"`
	HeadSHA    string `json:"head_sha"`
}

type evidenceTestJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url,omitempty"`
}

type evidenceTestStatus struct {
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"target_url,omitempty"`
}

type evidenceFixture struct {
	checkStatus  int
	checkPages   map[int][]map[string]string
	checkTotal   int
	actionsCode  int
	runPages     map[int][]evidenceTestRun
	runsTotal    int
	jobPages     map[int64]map[int][]evidenceTestJob
	jobTotals    map[int64]int
	statusPages  map[int][]evidenceTestStatus
	statusTotal  int
	statusState  string
	required     []string
	requiredCode int
	requiredBody string
	pull         bool
}

func greenRun(id, workflowID int64, name string, attempt int) evidenceTestRun {
	return evidenceTestRun{ID: id, WorkflowID: workflowID, RunAttempt: attempt, Name: name, Status: "completed", Conclusion: "success", HeadSHA: evidenceTestSHA}
}

func greenJob(id int64, name string) evidenceTestJob {
	return evidenceTestJob{ID: id, Name: name, Status: "completed", Conclusion: "success"}
}

func newEvidenceServer(t *testing.T, fixture evidenceFixture) *httptest.Server {
	t.Helper()
	if fixture.checkStatus == 0 {
		fixture.checkStatus = http.StatusOK
	}
	if fixture.actionsCode == 0 {
		fixture.actionsCode = http.StatusOK
	}
	if fixture.statusState == "" {
		fixture.statusState = "success"
	}
	if fixture.requiredCode == 0 {
		fixture.requiredCode = http.StatusNotFound
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/check-runs"):
			w.WriteHeader(fixture.checkStatus)
			if fixture.checkStatus != http.StatusOK {
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": fixture.checkTotal, "check_runs": fixture.checkPages[page]})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runs"):
			w.WriteHeader(fixture.actionsCode)
			if fixture.actionsCode != http.StatusOK {
				return
			}
			if got := r.URL.Query().Get("head_sha"); got != evidenceTestSHA {
				t.Fatalf("head_sha=%q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": fixture.runsTotal, "workflow_runs": fixture.runPages[page]})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/jobs"):
			parts := strings.Split(r.URL.Path, "/")
			runID, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
			if err != nil {
				t.Fatalf("run id path=%s", r.URL.Path)
			}
			if r.URL.Query().Get("filter") != "latest" {
				t.Fatalf("jobs filter=%q", r.URL.Query().Get("filter"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": fixture.jobTotals[runID], "jobs": fixture.jobPages[runID][page]})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]any{"state": fixture.statusState, "total_count": fixture.statusTotal, "statuses": fixture.statusPages[page]})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/branches/main/protection/required_status_checks"):
			w.WriteHeader(fixture.requiredCode)
			if fixture.requiredCode == http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]any{"contexts": fixture.required, "checks": []any{}})
			} else if fixture.requiredBody != "" {
				_, _ = w.Write([]byte(fixture.requiredBody))
			}
		case fixture.pull && r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls/7":
			_, _ = fmt.Fprintf(w, `{"number":7,"state":"open","merged":false,"mergeable":true,"html_url":"https://github.com/acme/demo/pull/7","head":{"ref":"feature","sha":"%s"},"base":{"ref":"main","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`, evidenceTestSHA)
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
}

func collectEvidence(t *testing.T, fixture evidenceFixture) (githubCheckSummary, error) {
	t.Helper()
	server := newEvidenceServer(t, fixture)
	defer server.Close()
	client := NewGitHubClient(server.URL, "token", "acme", "org", "private")
	return client.checkSummaryForBase(context.Background(), "demo", evidenceTestSHA, "main")
}

func TestGitHubEvidenceUsesChecksAPIWhenAvailable(t *testing.T) {
	summary, err := collectEvidence(t, evidenceFixture{
		checkTotal: 1,
		checkPages: map[int][]map[string]string{1: {{"name": "Verify", "status": "completed", "conclusion": "success"}}},
	})
	if err != nil || summary.Source != "checks_api" || summary.RunsTotal != 1 || summary.JobsTotal != 0 || !summary.EvidenceComplete || !summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceFallsBackToGreenActions(t *testing.T) {
	run := greenRun(10, 100, "CI", 1)
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1, runPages: map[int][]evidenceTestRun{1: {run}},
		jobTotals: map[int64]int{10: 1}, jobPages: map[int64]map[int][]evidenceTestJob{10: {1: {greenJob(20, "Verify")}}},
	})
	if err != nil || summary.Source != "actions_fallback" || summary.RunsTotal != 1 || summary.JobsTotal != 1 || !summary.EvidenceComplete || !summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceActionsPending(t *testing.T) {
	run := greenRun(10, 100, "CI", 1)
	run.Status, run.Conclusion = "in_progress", ""
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1, runPages: map[int][]evidenceTestRun{1: {run}},
		jobTotals: map[int64]int{10: 1}, jobPages: map[int64]map[int][]evidenceTestJob{10: {1: {{ID: 20, Name: "Verify", Status: "queued"}}}},
	})
	if err != nil || summary.Pending == 0 || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceActionsFailure(t *testing.T) {
	run := greenRun(10, 100, "CI", 1)
	run.Conclusion = "failure"
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1, runPages: map[int][]evidenceTestRun{1: {run}},
		jobTotals: map[int64]int{10: 1}, jobPages: map[int64]map[int][]evidenceTestJob{10: {1: {greenJob(20, "Verify")}}},
	})
	if err != nil || summary.Failed == 0 || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceJobFailureBlocksGreenRun(t *testing.T) {
	failedJob := greenJob(20, "Verify")
	failedJob.Conclusion = "failure"
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1, runPages: map[int][]evidenceTestRun{1: {greenRun(10, 100, "CI", 1)}},
		jobTotals: map[int64]int{10: 1}, jobPages: map[int64]map[int][]evidenceTestJob{10: {1: {failedJob}}},
	})
	if err != nil || summary.Failed == 0 || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceUsesLatestGreenRerun(t *testing.T) {
	failed := greenRun(10, 100, "CI", 1)
	failed.Conclusion = "failure"
	green := greenRun(11, 100, "CI", 2)
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   2, runPages: map[int][]evidenceTestRun{1: {failed, green}},
		jobTotals: map[int64]int{11: 1}, jobPages: map[int64]map[int][]evidenceTestJob{11: {1: {greenJob(21, "Verify")}}},
	})
	if err != nil || summary.RunsTotal != 1 || summary.Failed != 0 || !summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceLatestRerunPendingDoesNotHide(t *testing.T) {
	green := greenRun(10, 100, "CI", 1)
	pending := greenRun(11, 100, "CI", 2)
	pending.Status, pending.Conclusion = "waiting", ""
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   2, runPages: map[int][]evidenceTestRun{1: {green, pending}},
		jobTotals: map[int64]int{11: 1}, jobPages: map[int64]map[int][]evidenceTestJob{11: {1: {{ID: 21, Name: "Verify", Status: "requested"}}}},
	})
	if err != nil || summary.RunsTotal != 1 || summary.Pending == 0 || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceCommitStatusFailureBlocks(t *testing.T) {
	summary, err := collectEvidence(t, evidenceFixture{
		checkTotal:  1,
		checkPages:  map[int][]map[string]string{1: {{"name": "Verify", "status": "completed", "conclusion": "success"}}},
		statusTotal: 1, statusState: "failure", statusPages: map[int][]evidenceTestStatus{1: {{Context: "legacy", State: "failure"}}},
	})
	if err != nil || summary.CommitStatuses != 1 || summary.Failed == 0 || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceZeroRunsIsIncomplete(t *testing.T) {
	summary, err := collectEvidence(t, evidenceFixture{checkStatus: http.StatusForbidden, runPages: map[int][]evidenceTestRun{}, jobPages: map[int64]map[int][]evidenceTestJob{}})
	if err != nil || summary.RunsTotal != 0 || summary.EvidenceComplete || summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceMissingRequiredWorkflowIsIncomplete(t *testing.T) {
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1, runPages: map[int][]evidenceTestRun{1: {greenRun(10, 100, "CI", 1)}},
		jobTotals: map[int64]int{10: 1}, jobPages: map[int64]map[int][]evidenceTestJob{10: {1: {greenJob(20, "Build")}}},
		requiredCode: http.StatusOK, required: []string{"CI / Verify"},
	})
	if err != nil || summary.EvidenceComplete || summary.AllChecksGreen || !strings.Contains(strings.Join(summary.Lines, "\n"), "missing_required: CI / Verify") {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubEvidenceActions403FailsClosed(t *testing.T) {
	_, err := collectEvidence(t, evidenceFixture{checkStatus: http.StatusForbidden, actionsCode: http.StatusForbidden})
	if err == nil || !strings.Contains(err.Error(), "Actions fallback -> HTTP 403") {
		t.Fatalf("err=%v", err)
	}
}

func TestGitHubEvidenceProcessesPagination(t *testing.T) {
	run1 := greenRun(10, 100, "CI", 1)
	run2 := greenRun(11, 200, "Security", 1)
	summary, err := collectEvidence(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   2, runPages: map[int][]evidenceTestRun{1: {run1}, 2: {run2}},
		jobTotals: map[int64]int{10: 2, 11: 1},
		jobPages: map[int64]map[int][]evidenceTestJob{
			10: {1: {greenJob(20, "Verify")}, 2: {greenJob(21, "Race")}},
			11: {1: {greenJob(22, "CodeQL")}},
		},
		statusTotal: 2,
		statusPages: map[int][]evidenceTestStatus{1: {{Context: "one", State: "success"}}, 2: {{Context: "two", State: "success"}}},
	})
	if err != nil || summary.RunsTotal != 2 || summary.JobsTotal != 3 || summary.CommitStatuses != 2 || !summary.EvidenceComplete || !summary.AllChecksGreen {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestGitHubMergePreviewRejectsIncompleteEvidence(t *testing.T) {
	server := newEvidenceServer(t, evidenceFixture{checkStatus: http.StatusForbidden, pull: true})
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	if preview, err := svc.SourcePullRequestMergePreview("demo", 7); err == nil || preview != "" || !strings.Contains(err.Error(), "not completely green") {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
}
