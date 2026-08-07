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

func TestEdgeReleaseMaintenanceCancelsObsoleteRunBeforePolicyWrite(t *testing.T) {
	mainSHA := strings.Repeat("b", 40)
	oldSHA := strings.Repeat("a", 40)
	const runID int64 = 31207230123
	protectedBranches := true
	customPolicies := false
	cancelled := false
	policies := map[int64]string{8: "release/*"}
	events := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/main":
			fmt.Fprintf(w, `{"object":{"sha":"%s"}}`, mainSHA)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/branches/main/protection":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/rules/branches/main":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/environments/edge-release":
			fmt.Fprintf(w, `{"name":"edge-release","protection_rules":[],"deployment_branch_policy":{"protected_branches":%t,"custom_branch_policies":%t}}`, protectedBranches, customPolicies)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/environments/edge-release/deployment-branch-policies":
			if !customPolicies {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprintf(w, `{"branch_policies":[`)
			first := true
			for id, name := range policies {
				if !first {
					w.Write([]byte(","))
				}
				first = false
				fmt.Fprintf(w, `{"id":%d,"name":"%s"}`, id, name)
			}
			w.Write([]byte(`]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/actions/workflows/edge-release.yml/runs":
			status, conclusion := "waiting", ""
			if cancelled {
				status, conclusion = "completed", "cancelled"
			}
			fmt.Fprintf(w, `{"workflow_runs":[{"id":%d,"workflow_id":42,"head_branch":"main","head_sha":"%s","event":"workflow_dispatch","status":"%s","conclusion":"%s"}]}`, runID, oldSHA, status, conclusion)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/releases":
			w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/releases/tags/stable":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/acme/mcp-devbox/actions/runs/%d", runID):
			status, conclusion := "waiting", ""
			if cancelled {
				status, conclusion = "completed", "cancelled"
			}
			fmt.Fprintf(w, `{"id":%d,"workflow_id":42,"head_branch":"main","head_sha":"%s","event":"workflow_dispatch","status":"%s","conclusion":"%s"}`, runID, oldSHA, status, conclusion)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/actions/workflows/edge-release.yml":
			w.Write([]byte(`{"id":42,"name":"Publish signed P15 Edge release","path":".github/workflows/edge-release.yml","state":"active"}`))
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/acme/mcp-devbox/actions/runs/%d/pending_deployments", runID):
			w.Write([]byte(`[{"environment":{"name":"edge-release"},"wait_timer":0,"current_user_can_approve":false,"reviewers":[]}]`))
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/acme/mcp-devbox/actions/runs/%d/jobs", runID):
			w.Write([]byte(`{"jobs":[{"name":"Build, verify, and publish official stable bundle","status":"waiting","conclusion":"","steps":[]}]}`))
		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/acme/mcp-devbox/actions/runs/%d/cancel", runID):
			events = append(events, "cancel")
			cancelled = true
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/mcp-devbox/environments/edge-release":
			if !cancelled {
				t.Fatal("environment was changed before obsolete run cancellation")
			}
			var body struct {
				WaitTimer        int   `json:"wait_timer"`
				Reviewers        []any `json:"reviewers"`
				DeploymentPolicy struct {
					Protected bool `json:"protected_branches"`
					Custom    bool `json:"custom_branch_policies"`
				} `json:"deployment_branch_policy"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WaitTimer != 0 || len(body.Reviewers) != 0 || body.DeploymentPolicy.Protected || !body.DeploymentPolicy.Custom {
				t.Fatalf("unexpected environment body: %#v err=%v", body, err)
			}
			events = append(events, "policy")
			protectedBranches = false
			customPolicies = true
			w.Write([]byte(`{"name":"edge-release"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/mcp-devbox/environments/edge-release/deployment-branch-policies":
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "main" {
				t.Fatalf("unexpected create body: %#v err=%v", body, err)
			}
			policies[7] = "main"
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/acme/mcp-devbox/environments/edge-release/deployment-branch-policies/8":
			delete(policies, 8)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))

	status, err := svc.SourceEdgeReleaseStatus()
	if err != nil || !strings.Contains(status, "main_protected: false") || !strings.Contains(status, "pending_environment: run=31207230123 name=edge-release") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	preview, err := svc.SourceEdgeReleaseMaintenancePreview()
	if err != nil || !strings.Contains(preview, "obsolete_active_runs: 1") || !strings.Contains(preview, oldSHA) {
		t.Fatalf("preview=%q err=%v", preview, err)
	}
	result, err := svc.SourceEdgeReleaseMaintenanceApply(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "cancel,policy" {
		t.Fatalf("write order=%v", events)
	}
	if protectedBranches || !customPolicies || len(policies) != 1 || policies[7] != "main" {
		t.Fatalf("final policy protected=%t custom=%t policies=%v", protectedBranches, customPolicies, policies)
	}
	if !strings.Contains(result, "main_protected: false") || !strings.Contains(result, "deployment_branch_policy: main") || !strings.Contains(result, "obsolete_runs_cancelled: 1") {
		t.Fatalf("result=%q", result)
	}
}

func TestEdgeReleaseMaintenanceRefusesProtectedMain(t *testing.T) {
	mainSHA := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/mcp-devbox/git/ref/heads/main":
			fmt.Fprintf(w, `{"object":{"sha":"%s"}}`, mainSHA)
		case "/repos/acme/mcp-devbox/branches/main/protection":
			w.Write([]byte(`{}`))
		case "/repos/acme/mcp-devbox/rules/branches/main":
			w.Write([]byte(`[]`))
		case "/repos/acme/mcp-devbox/environments/edge-release":
			w.Write([]byte(`{"name":"edge-release","protection_rules":[],"deployment_branch_policy":{"protected_branches":true,"custom_branch_policies":false}}`))
		case "/repos/acme/mcp-devbox/environments/edge-release/deployment-branch-policies":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/mcp-devbox/actions/workflows/edge-release.yml/runs":
			w.Write([]byte(`{"workflow_runs":[]}`))
		case "/repos/acme/mcp-devbox/releases":
			w.Write([]byte(`[]`))
		case "/repos/acme/mcp-devbox/releases/tags/stable":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(server.URL, "token", "acme", "user", "private"))
	if _, err := svc.SourceEdgeReleaseMaintenancePreview(); err == nil || !strings.Contains(err.Error(), "never changes branch protection") {
		t.Fatalf("expected protected-main refusal, got %v", err)
	}
}
