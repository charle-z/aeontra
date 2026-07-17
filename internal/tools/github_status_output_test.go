package tools

import (
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSourcePullRequestStatusReportsClosedActionsFallbackSummary(t *testing.T) {
	server := newEvidenceServer(t, evidenceFixture{
		checkStatus: http.StatusForbidden,
		runsTotal:   1,
		runPages:    map[int][]evidenceTestRun{1: {greenRun(10, 100, "CI", 1)}},
		jobTotals:   map[int64]int{10: 1},
		jobPages:    map[int64]map[int][]evidenceTestJob{10: {1: {greenJob(20, "Verify")}}},
		pull:        true,
	})
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	status, err := svc.SourcePullRequestStatus("demo", 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"source: actions_fallback",
		"runs_total: 1",
		"jobs_total: 1",
		"passed: 2",
		"pending: 0",
		"failed: 0",
		"commit_statuses: 0",
		"all_checks_green: true",
		"evidence_complete: true",
	} {
		if !strings.Contains(status, expected) {
			t.Fatalf("missing %q in %s", expected, status)
		}
	}
}
