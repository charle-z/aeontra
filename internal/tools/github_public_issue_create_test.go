package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubPublicIssueCreateRevalidatesUpstream(t *testing.T) {
	defaultBranch := "main"
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/upstream/demo":
			_, _ = w.Write([]byte(`{"full_name":"upstream/demo","private":false,"visibility":"public","default_branch":"` + defaultBranch + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/upstream/demo/issues":
			created = true
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	preview, err := svc.SourcePublicIssueCreatePreview("upstream", "demo", "Bug", "### Description\nBroken")
	if err != nil {
		t.Fatal(err)
	}
	defaultBranch = "next"
	if _, err := svc.SourcePublicIssueCreate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "upstream repository changed") || created {
		t.Fatalf("expected upstream revalidation failure, err=%v created=%t", err, created)
	}
}

func TestGitHubPublicIssueCreateRejectsEmptyBody(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient("https://example.invalid", "token", "acme", "user", "private"))
	if _, err := svc.SourcePublicIssueCreatePreview("upstream", "demo", "Bug", " "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-body rejection, got %v", err)
	}
}
