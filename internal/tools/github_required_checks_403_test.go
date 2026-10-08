package tools

import (
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubRequiredChecks403KeepsStatusVisibleAndMergeClosed(t *testing.T) {
	fixture := evidenceFixture{
		checkStatus:  http.StatusForbidden,
		runsTotal:    1,
		runPages:     map[int][]evidenceTestRun{1: {greenRun(10, 100, "CI", 1)}},
		jobTotals:    map[int64]int{10: 1},
		jobPages:     map[int64]map[int][]evidenceTestJob{10: {1: {greenJob(20, "Verify")}}},
		requiredCode: http.StatusForbidden,
		pull:         true,
	}
	server := newEvidenceServer(t, fixture)
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
		"all_checks_green: false",
		"evidence_complete: false",
		"required_checks: unavailable",
	} {
		if !strings.Contains(status, expected) {
			t.Fatalf("missing %q in %s", expected, status)
		}
	}
	if preview, err := svc.SourcePullRequestMergePreview("demo", 7); err == nil || preview != "" || !strings.Contains(err.Error(), "not completely green") {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
}

func TestGitHubUnavailablePrivateBranchProtectionAcceptsCompleteGreenEvidence(t *testing.T) {
	fixture := evidenceFixture{
		checkTotal:   1,
		checkPages:   map[int][]map[string]string{1: {{"name": "Verify", "status": "completed", "conclusion": "success"}}},
		requiredCode: http.StatusForbidden,
		requiredBody: `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-status-checks-protection","status":"403"}`,
		pull:         true,
	}
	server := newEvidenceServer(t, fixture)
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	status, err := svc.SourcePullRequestStatus("demo", 7)
	if err != nil || !strings.Contains(status, "all_checks_green: true") || !strings.Contains(status, "evidence_complete: true") || !strings.Contains(status, "required_checks: feature_unavailable") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	preview, err := svc.SourcePullRequestMergePreview("demo", 7)
	if err != nil || !strings.Contains(preview, "evidence_complete: true") {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
}
