package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubPullRequestWorkflowIsOwnerBoundPlannedAndGreen(t *testing.T) {
	headSHA := strings.Repeat("a", 40)
	baseSHA := strings.Repeat("b", 40)
	mergeSHA := strings.Repeat("c", 40)
	created := false
	merged := false
	defaultBranch := "mvp"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/mvp":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls":
			if created {
				_, _ = w.Write([]byte(`[{"number":7,"state":"open","html_url":"https://github.com/acme/demo/pull/7","head":{"ref":"mvp","sha":"` + headSHA + `"},"base":{"ref":"main","sha":"` + baseSHA + `"}}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/demo/pulls":
			created = true
			_, _ = w.Write([]byte(`{"number":7,"state":"open","html_url":"https://github.com/acme/demo/pull/7","head":{"ref":"mvp","sha":"` + headSHA + `"},"base":{"ref":"main","sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"state":"open","merged":false,"mergeable":true,"html_url":"https://github.com/acme/demo/pull/7","head":{"ref":"mvp","sha":"` + headSHA + `"},"base":{"ref":"main","sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/commits/"+headSHA+"/check-runs":
			_, _ = w.Write([]byte(`{"total_count":2,"check_runs":[{"name":"CI","status":"completed","conclusion":"success","html_url":"https://github.com/acme/demo/actions/1"},{"name":"Security","status":"completed","conclusion":"success","html_url":"https://github.com/acme/demo/actions/2"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/commits/"+headSHA+"/status":
			_, _ = w.Write([]byte(`{"state":"success","statuses":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/branches/main/protection/required_status_checks":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/demo/pulls/7/merge":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["merge_method"] != "merge" || body["sha"] != headSHA {
				t.Fatalf("unexpected merge body: %#v err=%v", body, err)
			}
			merged = true
			_, _ = w.Write([]byte(`{"sha":"` + mergeSHA + `","merged":true,"message":"Pull Request successfully merged"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo":
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","default_branch":"` + defaultBranch + `"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/demo":
			defaultBranch = "main"
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","default_branch":"main"}`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))

	preview, err := svc.SourcePullRequestCreatePreview("demo", "mvp", "main", "MVP", "safe body")
	if err != nil || created || !strings.Contains(preview, headSHA) {
		t.Fatalf("create preview=%q err=%v created=%t", preview, err, created)
	}
	createdResult, err := svc.SourcePullRequestCreate(field(preview, "plan_id"), true)
	if err != nil || !created || !strings.Contains(createdResult, "pull_request: 7") {
		t.Fatalf("create result=%q err=%v created=%t", createdResult, err, created)
	}
	status, err := svc.SourcePullRequestStatus("demo", 7)
	if err != nil || !strings.Contains(status, "all_checks_green: true") || !strings.Contains(status, "runs_total: 2") || !strings.Contains(status, "evidence_complete: true") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	mergePreview, err := svc.SourcePullRequestMergePreview("demo", 7)
	if err != nil || merged {
		t.Fatalf("merge preview=%q err=%v merged=%t", mergePreview, err, merged)
	}
	mergeResult, err := svc.SourcePullRequestMerge(field(mergePreview, "plan_id"), true)
	if err != nil || !merged || !strings.Contains(mergeResult, mergeSHA) {
		t.Fatalf("merge result=%q err=%v merged=%t", mergeResult, err, merged)
	}
	branchPreview, err := svc.SourceDefaultBranchUpdatePreview("demo", "main")
	if err != nil || defaultBranch != "mvp" {
		t.Fatalf("branch preview=%q err=%v default=%s", branchPreview, err, defaultBranch)
	}
	branchResult, err := svc.SourceDefaultBranchUpdate(field(branchPreview, "plan_id"), true)
	if err != nil || defaultBranch != "main" || !strings.Contains(branchResult, "default_branch: main") {
		t.Fatalf("branch result=%q err=%v default=%s", branchResult, err, defaultBranch)
	}
}

func TestGitHubPullRequestCreateRevalidatesHeadSHA(t *testing.T) {
	headSHA := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/git/ref/heads/mvp"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case strings.Contains(r.URL.Path, "/git/ref/heads/main"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + strings.Repeat("b", 40) + `"}}`))
		case r.URL.Path == "/repos/acme/demo/pulls":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourcePullRequestCreatePreview("demo", "mvp", "main", "MVP", "")
	if err != nil {
		t.Fatal(err)
	}
	headSHA = strings.Repeat("d", 40)
	if _, err := svc.SourcePullRequestCreate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "head changed") {
		t.Fatalf("expected head revalidation failure, got %v", err)
	}
}
