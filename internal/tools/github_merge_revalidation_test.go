package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubMergeExecutionRevalidatesFallbackEvidence(t *testing.T) {
	phase := 0
	merged := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"state":"open","merged":false,"mergeable":true,"html_url":"https://github.com/acme/demo/pull/7","head":{"ref":"feature","sha":"` + evidenceTestSHA + `"},"base":{"ref":"main","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/check-runs"):
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runs"):
			if phase == 0 {
				_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "workflow_runs": []evidenceTestRun{greenRun(10, 100, "CI", 1)}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "workflow_runs": []evidenceTestRun{}})
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions/runs/10/jobs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "jobs": []evidenceTestJob{greenJob(20, "Verify")}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"state":"success","total_count":0,"statuses":[]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/branches/main/protection/required_status_checks"):
			http.NotFound(w, r)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/pulls/7/merge"):
			merged = true
			_, _ = w.Write([]byte(`{"sha":"cccccccccccccccccccccccccccccccccccccccc","merged":true,"message":"merged"}`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourcePullRequestMergePreview("demo", 7)
	if err != nil {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
	phase = 1
	if result, err := svc.SourcePullRequestMerge(field(preview, "plan_id"), true); err == nil || result != "" || !strings.Contains(err.Error(), "checks changed") || merged {
		t.Fatalf("result=%q err=%v merged=%t", result, err, merged)
	}
}
