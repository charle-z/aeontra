package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubPublicOSSWorkflowIsPlannedRevalidatedAndOwnerBound(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	forkCreated := false
	issueCreated := false
	commentCreated := false
	pullCreated := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public","default_branch":"main","fork":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo":
			if !forkCreated {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"visibility":"public","default_branch":"main","fork":true,"parent":{"full_name":"upstream/demo"},"permissions":{"admin":true,"push":true,"pull":true}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/forks":
			forkCreated = true
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"visibility":"public","default_branch":"main","fork":true,"parent":{"full_name":"upstream/demo"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/issues":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["title"] != "Native result bug" || body["body"] != "### Description\nBroken" {
				t.Fatalf("unexpected issue body: %#v err=%v", body, err)
			}
			issueCreated = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":10,"state":"open","title":"Native result bug","html_url":"https://github.com/upstream/demo/issues/10","updated_at":"2026-08-05T20:00:00Z","comments":0,"assignees":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9":
			_, _ = w.Write([]byte(`{"number":9,"state":"open","title":"Fix it","html_url":"https://github.com/upstream/demo/issues/9","updated_at":"2026-08-05T20:00:00Z","comments":0,"assignees":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9/timeline":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["body"] != "/assign acme" {
				t.Fatalf("unexpected comment body: %#v err=%v", body, err)
			}
			commentCreated = true
			_, _ = w.Write([]byte(`{"id":11,"html_url":"https://github.com/upstream/demo/issues/9#issuecomment-11","body":"/assign acme","user":{"login":"acme"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/fix/test":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/compare/"+baseSHA+"..."+headSHA:
			_, _ = w.Write([]byte(`{"status":"ahead","ahead_by":1,"merge_base_commit":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls":
			if pullCreated {
				_, _ = w.Write([]byte(`[{"number":17,"state":"open","html_url":"https://github.com/upstream/demo/pull/17","head":{"ref":"fix/test","sha":"` + headSHA + `","repo":{"full_name":"acme/demo"},"user":{"login":"acme"}},"base":{"ref":"main","sha":"` + baseSHA + `"}}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/pulls":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["head"] != "acme:fix/test" || body["base"] != "main" || body["draft"] != false {
				t.Fatalf("unexpected pull body: %#v err=%v", body, err)
			}
			pullCreated = true
			_, _ = w.Write([]byte(`{"number":17,"state":"open","html_url":"https://github.com/upstream/demo/pull/17","head":{"ref":"fix/test","sha":"` + headSHA + `","repo":{"full_name":"acme/demo"},"user":{"login":"acme"}},"base":{"ref":"main","sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17":
			_, _ = w.Write([]byte(`{"number":17,"state":"open","merged":false,"mergeable":true,"html_url":"https://github.com/upstream/demo/pull/17","head":{"ref":"fix/test","sha":"` + headSHA + `","repo":{"full_name":"acme/demo"},"user":{"login":"acme"}},"base":{"ref":"main","sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/commits/"+headSHA+"/check-runs":
			_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[{"name":"CI","status":"completed","conclusion":"success","html_url":"https://github.com/upstream/demo/actions/1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/commits/"+headSHA+"/status":
			_, _ = w.Write([]byte(`{"state":"success","total_count":0,"statuses":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17/reviews":
			_, _ = w.Write([]byte(`[{"id":1,"state":"APPROVED","body":"LGTM","user":{"login":"maintainer"},"submitted_at":"2026-08-05T21:00:00Z"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17/comments":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/17/comments":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))

	issue, err := svc.SourcePublicIssueStatus("upstream", "demo", 9)
	if err != nil || !strings.Contains(issue, "linked_pull_requests: 0") || !strings.Contains(issue, "assignees: none") {
		t.Fatalf("issue=%q err=%v", issue, err)
	}
	issuePreview, err := svc.SourcePublicIssueCreatePreview("upstream", "demo", "Native result bug", "### Description\nBroken")
	if err != nil || issueCreated {
		t.Fatalf("issue preview=%q err=%v created=%t", issuePreview, err, issueCreated)
	}
	createdIssue, err := svc.SourcePublicIssueCreate(field(issuePreview, "plan_id"), true)
	if err != nil || !issueCreated || !strings.Contains(createdIssue, "issue: 10") {
		t.Fatalf("issue create=%q err=%v created=%t", createdIssue, err, issueCreated)
	}
	forkPreview, err := svc.SourcePublicForkCreatePreview("upstream", "demo")
	if err != nil || forkCreated {
		t.Fatalf("fork preview=%q err=%v created=%t", forkPreview, err, forkCreated)
	}
	fork, err := svc.SourcePublicForkCreate(field(forkPreview, "plan_id"), true)
	if err != nil || !forkCreated || !strings.Contains(fork, "parent: upstream/demo") {
		t.Fatalf("fork=%q err=%v created=%t", fork, err, forkCreated)
	}
	commentPreview, err := svc.SourcePublicIssueCommentPreview("upstream", "demo", 9, "/assign acme")
	if err != nil || commentCreated {
		t.Fatalf("comment preview=%q err=%v created=%t", commentPreview, err, commentCreated)
	}
	comment, err := svc.SourcePublicIssueComment(field(commentPreview, "plan_id"), true)
	if err != nil || !commentCreated || !strings.Contains(comment, "issue_comment: 11") {
		t.Fatalf("comment=%q err=%v created=%t", comment, err, commentCreated)
	}
	pullPreview, err := svc.SourceCrossRepoPullRequestCreatePreview("upstream", "demo", "fix/test", "main", "Fix test", "Closes #9", false)
	if err != nil || pullCreated || !strings.Contains(pullPreview, headSHA) || !strings.Contains(pullPreview, baseSHA) {
		t.Fatalf("pull preview=%q err=%v created=%t", pullPreview, err, pullCreated)
	}
	pull, err := svc.SourceCrossRepoPullRequestCreate(field(pullPreview, "plan_id"), true)
	if err != nil || !pullCreated || !strings.Contains(pull, "pull_request: 17") {
		t.Fatalf("pull=%q err=%v created=%t", pull, err, pullCreated)
	}
	status, err := svc.SourcePublicPullRequestStatus("upstream", "demo", 17)
	if err != nil || !strings.Contains(status, "all_checks_green: true") || !strings.Contains(status, "review: maintainer | state=APPROVED") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestGitHubPublicOSSRejectsConfiguredOwnerAsUpstream(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient("https://example.invalid", "token", "acme", "user", "private"))
	if _, err := svc.SourcePublicForkCreatePreview("acme", "demo"); err == nil || !strings.Contains(err.Error(), "external upstream") {
		t.Fatalf("expected owner-bound rejection, got %v", err)
	}
}

func TestGitHubPublicOSSAcceptsDivergedForkWithUniqueCommits(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	mergeBaseSHA := strings.Repeat("c", 40)
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public","default_branch":"main"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo":
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"visibility":"public","fork":true,"parent":{"full_name":"upstream/demo"},"permissions":{"push":true}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/fix/test":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/demo/compare/"):
			_, _ = w.Write([]byte(`{"status":"diverged","ahead_by":1,"behind_by":3,"merge_base_commit":{"sha":"` + mergeBaseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/pulls":
			created = true
			_, _ = w.Write([]byte(`{"number":17,"state":"open","html_url":"https://github.com/upstream/demo/pull/17","head":{"ref":"fix/test","sha":"` + headSHA + `"},"base":{"ref":"main","sha":"` + baseSHA + `"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourceCrossRepoPullRequestCreatePreview("upstream", "demo", "fix/test", "main", "Fix", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SourceCrossRepoPullRequestCreate(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected pull request creation")
	}
}

func TestValidateCrossRepoHeadContribution(t *testing.T) {
	validMergeBase := strings.Repeat("c", 40)
	tests := []struct {
		name       string
		comparison githubCompareResponse
		wantErr    bool
	}{
		{
			name: "ahead with unique commit and merge base",
			comparison: func() githubCompareResponse {
				result := githubCompareResponse{Status: "ahead", AheadBy: 1}
				result.MergeBaseCommit.SHA = validMergeBase
				return result
			}(),
		},
		{
			name:       "ahead without merge base",
			comparison: githubCompareResponse{Status: "ahead", AheadBy: 1},
			wantErr:    true,
		},
		{
			name:       "ahead without unique commit",
			comparison: githubCompareResponse{Status: "ahead"},
			wantErr:    true,
		},
		{
			name:       "identical",
			comparison: githubCompareResponse{Status: "identical"},
			wantErr:    true,
		},
		{
			name:       "behind",
			comparison: githubCompareResponse{Status: "behind"},
			wantErr:    true,
		},
		{
			name: "diverged with unique commit and merge base",
			comparison: func() githubCompareResponse {
				result := githubCompareResponse{Status: "diverged", AheadBy: 1}
				result.MergeBaseCommit.SHA = validMergeBase
				return result
			}(),
		},
		{
			name: "diverged without unique commit",
			comparison: func() githubCompareResponse {
				result := githubCompareResponse{Status: "diverged"}
				result.MergeBaseCommit.SHA = validMergeBase
				return result
			}(),
			wantErr: true,
		},
		{
			name:       "diverged without shared history evidence",
			comparison: githubCompareResponse{Status: "diverged", AheadBy: 1},
			wantErr:    true,
		},
		{
			name: "diverged with malformed merge base",
			comparison: func() githubCompareResponse {
				result := githubCompareResponse{Status: "diverged", AheadBy: 1}
				result.MergeBaseCommit.SHA = "not-a-sha"
				return result
			}(),
			wantErr: true,
		},
		{
			name: "unknown status",
			comparison: func() githubCompareResponse {
				result := githubCompareResponse{Status: "unknown", AheadBy: 1}
				result.MergeBaseCommit.SHA = validMergeBase
				return result
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCrossRepoHeadContribution(tt.comparison)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateCrossRepoHeadContribution() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestGitHubPublicOSSRejectsPrivateUpstreamAndWrongFork(t *testing.T) {
	tests := []struct {
		name string
		repo string
		want string
	}{
		{name: "private upstream", repo: `{"full_name":"upstream/demo","private":true,"visibility":"private"}`, want: "not public"},
		{name: "wrong fork parent", repo: `{"full_name":"upstream/demo","private":false,"visibility":"public","default_branch":"main"}`, want: "not the expected fork"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/upstream/demo":
					_, _ = w.Write([]byte(tt.repo))
				case "/repos/acme/demo":
					_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"visibility":"public","fork":true,"parent":{"full_name":"someone/demo"},"permissions":{"push":true}}`))
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			svc, _ := newTestService(t, config.ModeAllow)
			svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
			if _, err := svc.SourcePublicForkCreatePreview("upstream", "demo"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestGitHubPublicIssueCommentRevalidatesConversation(t *testing.T) {
	updatedAt := "2026-08-05T20:00:00Z"
	posted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9":
			_, _ = w.Write([]byte(`{"number":9,"state":"open","updated_at":"` + updatedAt + `","comments":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			posted = true
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourcePublicIssueCommentPreview("upstream", "demo", 9, "claim")
	if err != nil {
		t.Fatal(err)
	}
	updatedAt = "2026-08-05T20:01:00Z"
	if _, err := svc.SourcePublicIssueComment(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "state changed") || posted {
		t.Fatalf("expected revalidation failure, err=%v posted=%t", err, posted)
	}
}

func TestGitHubPublicIssueCommentRevalidatesPublicUpstream(t *testing.T) {
	public := true
	posted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			if public {
				_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public"}`))
			} else {
				_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":true,"visibility":"private"}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9":
			_, _ = w.Write([]byte(`{"number":9,"state":"open","updated_at":"2026-08-05T20:00:00Z","comments":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/issues/9/comments":
			posted = true
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourcePublicIssueCommentPreview("upstream", "demo", 9, "claim")
	if err != nil {
		t.Fatal(err)
	}
	public = false
	if _, err := svc.SourcePublicIssueComment(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "not public") || posted {
		t.Fatalf("expected public upstream revalidation failure, err=%v posted=%t", err, posted)
	}
}

func TestGitHubCrossRepoPullRequestRevalidatesHeadSHA(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public","default_branch":"main"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo":
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"visibility":"public","fork":true,"parent":{"full_name":"upstream/demo"},"permissions":{"push":true}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/git/ref/heads/fix/test":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + headSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/acme/demo/compare/"):
			_, _ = w.Write([]byte(`{"status":"ahead","ahead_by":1,"merge_base_commit":{"sha":"` + baseSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/pulls":
			created = true
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourceCrossRepoPullRequestCreatePreview("upstream", "demo", "fix/test", "main", "Fix", "", false)
	if err != nil {
		t.Fatal(err)
	}
	headSHA = strings.Repeat("c", 40)
	if _, err := svc.SourceCrossRepoPullRequestCreate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "head changed") || created {
		t.Fatalf("expected head revalidation failure, err=%v created=%t", err, created)
	}
}

func TestGitHubPublicPullRequestStatusFallsBackToUpstreamChecks(t *testing.T) {
	headSHA := strings.Repeat("d", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17":
			_, _ = w.Write([]byte(`{"number":17,"state":"open","merged":false,"mergeable":true,"html_url":"https://github.com/upstream/demo/pull/17","head":{"ref":"fix/test","sha":"` + headSHA + `","repo":{"full_name":"acme/demo"},"user":{"login":"acme"}},"base":{"ref":"main","sha":"` + strings.Repeat("a", 40) + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/demo/commits/"+headSHA+"/check-runs":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/commits/"+headSHA+"/check-runs":
			_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[{"name":"Upstream CI","status":"completed","conclusion":"success"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/commits/"+headSHA+"/status":
			_, _ = w.Write([]byte(`{"state":"success","total_count":0,"statuses":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17/reviews":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17/comments":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/issues/17/comments":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	status, err := svc.SourcePublicPullRequestStatus("upstream", "demo", 17)
	if err != nil || !strings.Contains(status, "all_checks_green: true") || !strings.Contains(status, "Upstream CI") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestGitHubPublicReviewReplyIsPlannedAndRevalidated(t *testing.T) {
	updatedAt := "2026-08-05T21:00:00Z"
	replied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/17":
			_, _ = w.Write([]byte(`{"number":17,"state":"open","head":{"sha":"` + strings.Repeat("a", 40) + `","repo":{"full_name":"acme/demo"}},"base":{"ref":"main"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo/pulls/comments/55":
			_, _ = w.Write([]byte(`{"id":55,"body":"Please rename this","path":"file.go","line":12,"html_url":"https://github.com/upstream/demo/pull/17#discussion_r55","pull_request_url":"https://api.github.com/repos/upstream/demo/pulls/17","created_at":"2026-08-05T20:00:00Z","updated_at":"` + updatedAt + `","user":{"login":"maintainer"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/pulls/17/comments/55/replies":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["body"] != "Renamed in the latest commit." {
				t.Fatalf("unexpected reply body: %#v err=%v", body, err)
			}
			replied = true
			_, _ = w.Write([]byte(`{"id":56,"body":"Renamed in the latest commit.","html_url":"https://github.com/upstream/demo/pull/17#discussion_r56","user":{"login":"acme"}}`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourcePublicReviewReplyPreview("upstream", "demo", 17, 55, "Renamed in the latest commit.")
	if err != nil || replied || !strings.Contains(preview, "file.go:12") {
		t.Fatalf("preview=%q err=%v replied=%t", preview, err, replied)
	}
	result, err := svc.SourcePublicReviewReply(field(preview, "plan_id"), true)
	if err != nil || !replied || !strings.Contains(result, "review_reply: 56") {
		t.Fatalf("result=%q err=%v replied=%t", result, err, replied)
	}

	preview, err = svc.SourcePublicReviewReplyPreview("upstream", "demo", 17, 55, "Another reply")
	if err != nil {
		t.Fatal(err)
	}
	updatedAt = "2026-08-05T21:01:00Z"
	replied = false
	if _, err := svc.SourcePublicReviewReply(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "review comment changed") || replied {
		t.Fatalf("expected review revalidation failure, err=%v replied=%t", err, replied)
	}
}
