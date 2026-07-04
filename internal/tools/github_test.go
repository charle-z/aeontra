package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitHubCreateRepo_DefaultsPrivateForUserOwner(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"demo","full_name":"charle-z/demo","private":true,"html_url":"https://github.com/charle-z/demo","clone_url":"https://github.com/charle-z/demo.git"}`))
	}))
	defer ts.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(ts.URL, "ghp_0123456789abcdefghijklmnopqrstuvwxyz", "charle-z", "user", "private"))
	out, err := svc.GitHubCreateRepo("demo", "Demo repo", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/user/repos" {
		t.Fatalf("path = %q, want /user/repos", gotPath)
	}
	if gotAuth != "Bearer ghp_0123456789abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("missing auth header: %q", gotAuth)
	}
	if gotBody.Name != "demo" || gotBody.Description != "Demo repo" || !gotBody.Private {
		t.Fatalf("bad create body: %#v", gotBody)
	}
	if !strings.Contains(out, "charle-z/demo") || strings.Contains(out, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestGitHubCreateRepo_PublicForOrgOwner(t *testing.T) {
	var gotPath string
	var gotBody struct {
		Private bool `json:"private"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":false,"html_url":"https://github.com/acme/demo"}`))
	}))
	defer ts.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "acme", "org", "private"))
	if _, err := svc.GitHubCreateRepo("demo", "", "public", false); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/orgs/acme/repos" {
		t.Fatalf("path = %q, want /orgs/acme/repos", gotPath)
	}
	if gotBody.Private {
		t.Fatal("public visibility should send private=false")
	}
}

func TestGitHubCreateRepo_AskRequiresApproval(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer ts.Close()

	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "charle-z", "user", "private"))
	out, err := svc.GitHubCreateRepo("demo", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask mode should require approval, got %q", out)
	}
	if calls != 0 {
		t.Fatalf("GitHub API should not be called before approval, calls=%d", calls)
	}
}

func TestGitHubCreateRepo_ErrorRedactsBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"bad ghp_0123456789abcdefghijklmnopqrstuvwxyz"}`))
	}))
	defer ts.Close()

	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "charle-z", "user", "private"))
	out, err := svc.GitHubCreateRepo("demo", "", "", false)
	if err == nil {
		t.Fatal("expected API error")
	}
	if strings.Contains(out+err.Error(), "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("error leaked secret: out=%q err=%v", out, err)
	}
}

func TestGitHubRepoInfo_ReadsRepo(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"full_name":"charle-z/demo","private":true,"html_url":"https://github.com/charle-z/demo","clone_url":"https://github.com/charle-z/demo.git","default_branch":"main"}`))
	}))
	defer ts.Close()

	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "charle-z", "user", "private"))
	out, err := svc.GitHubRepoInfo("demo")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/charle-z/demo" {
		t.Fatalf("path = %q, want /repos/charle-z/demo", gotPath)
	}
	if !strings.Contains(out, "default_branch: main") {
		t.Fatalf("repo info missing expected fields: %q", out)
	}
}

func TestGitHubTools_DisabledUnlessConfigured(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	if _, err := svc.GitHubCreateRepo("demo", "", "", true); err == nil {
		t.Fatal("github_create_repo should fail when not configured")
	}
	if _, err := svc.GitHubRepoInfo("demo"); err == nil {
		t.Fatal("github_repo_info should fail when not configured")
	}
}
