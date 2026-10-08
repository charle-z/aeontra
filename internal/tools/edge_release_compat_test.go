package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestPrivilegedCompatibilityRoutesOnlyEdgeReleaseMaintenance(t *testing.T) {
	mainSHA := strings.Repeat("a", 40)
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/git/ref/heads/main":
			fmt.Fprintf(w, `{"object":{"sha":"%s"}}`, mainSHA)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/branches/main/protection":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/rules/branches/main":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/environments/edge-release":
			w.Write([]byte(`{"name":"edge-release","protection_rules":[],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/environments/edge-release/deployment-branch-policies":
			w.Write([]byte(`{"branch_policies":[{"id":7,"name":"main"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/actions/workflows/edge-release.yml/runs":
			w.Write([]byte(`{"workflow_runs":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/releases":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/aeontra/releases/tags/stable":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/aeontra/environments/edge-release":
			writes++
			w.Write([]byte(`{"name":"edge-release"}`))
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithMaintainerProfile(MaintainerProfileCharleZProduction)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))

	if _, err := svc.PrivilegedTaskPreview("", "git-fetch", nil); err == nil || !strings.Contains(err.Error(), "disabled by administrator configuration") {
		t.Fatalf("ordinary privileged profile escaped disabled gate: %v", err)
	}
	if _, err := svc.PrivilegedTaskPreview("repo", "edge-release-maintenance", nil); err == nil || !strings.Contains(err.Error(), "accepts no repository") {
		t.Fatalf("compatibility profile accepted repository input: %v", err)
	}
	if _, err := svc.PrivilegedTaskPreview("", "edge-release-maintenance", map[string]string{"other": "value"}); err == nil || !strings.Contains(err.Error(), "accepts no repository or parameters") {
		t.Fatalf("compatibility profile accepted caller parameters: %v", err)
	}

	preview, err := svc.PrivilegedTaskPreview("", "edge-release-maintenance", nil)
	if err != nil || !strings.Contains(preview, "target_branch_policy: main") || !strings.Contains(preview, "obsolete_active_runs: 0") {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
	result, err := svc.PrivilegedTaskExecute(field(preview, "plan_id"), true)
	if err != nil || !strings.Contains(result, "deployment_branch_policy: main") || writes != 1 {
		t.Fatalf("result=%q err=%v writes=%d", result, err, writes)
	}
}
