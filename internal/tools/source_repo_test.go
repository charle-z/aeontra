package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSourceRepoInfoReportsExistenceAndPermission(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/demo" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"full_name":"acme/demo","visibility":"private","default_branch":"main","clone_url":"https://github.com/acme/demo.git","permissions":{"push":true,"pull":true}}`))
	}))
	defer ts.Close()
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "acme", "org", "private"))
	out, err := svc.SourceRepoInfo("demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"owner: acme", "full_name: acme/demo", "visibility: private", "default_branch: main", "exists: true", "viewer_permission: write"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestSourceRepoCreateUsesPreviewPlanAndDefaultsPrivate(t *testing.T) {
	created := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			created++
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","visibility":"private","private":true,"clone_url":"https://github.com/acme/demo.git","default_branch":"main"}`))
		}
	}))
	defer ts.Close()
	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "acme", "org", "private"))
	preview, err := svc.SourceRepoCreatePreview("demo", "", "safe description")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || !strings.Contains(preview, "visibility: private") || !strings.Contains(preview, "owner: acme") {
		t.Fatalf("bad preview or side effect: created=%d\n%s", created, preview)
	}
	planID := field(preview, "plan_id")
	out, err := svc.SourceRepoCreate(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || created != 0 {
		t.Fatalf("approval gate failed: out=%q err=%v created=%d", out, err, created)
	}
	out, err = svc.SourceRepoCreate(planID, true)
	if err != nil || !strings.Contains(out, "acme/demo") || created != 1 {
		t.Fatalf("create failed: out=%q err=%v created=%d", out, err, created)
	}
	if _, err := svc.SourceRepoCreate(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay must fail: %v", err)
	}
}

func TestSourceRepoCreateRejectsExistingInvalidExpiredAndChangedState(t *testing.T) {
	exists := true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && exists {
			_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":true}`))
			return
		}
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"full_name":"acme/demo","private":true}`))
	}))
	defer ts.Close()
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient(ts.URL, "token", "acme", "org", "private"))
	if _, err := svc.SourceRepoCreatePreview("../evil", "", ""); err == nil {
		t.Fatal("invalid repo name must fail")
	}
	if _, err := svc.SourceRepoCreatePreview("demo", "", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing repo must fail: %v", err)
	}
	exists = false
	preview, err := svc.SourceRepoCreatePreview("demo", "", "")
	if err != nil {
		t.Fatal(err)
	}
	planID := field(preview, "plan_id")
	exists = true
	if _, err := svc.SourceRepoCreate(planID, true); err == nil || !strings.Contains(err.Error(), "state changed") {
		t.Fatalf("changed existence must fail: %v", err)
	}
	exists = false
	preview, _ = svc.SourceRepoCreatePreview("demo", "", "")
	expired := field(preview, "plan_id")
	svc.plans.mu.Lock()
	svc.plans.plans[expired].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.SourceRepoCreate(expired, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired create plan must fail: %v", err)
	}
}

func TestSourceRepoConfigurationAndAPIErrorsAreExplicitAndRedacted(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithGitHub(NewGitHubClient("", "", "", "", ""))
	if _, err := svc.SourceRepoInfo("demo"); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "GITHUB_OWNER") {
		t.Fatalf("missing config must name variables: %v", err)
	}
	secret := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"` + secret + `"}`))
	}))
	defer ts.Close()
	svc.WithGitHub(NewGitHubClient(ts.URL, secret, "acme", "org", "private"))
	out, err := svc.SourceRepoInfo("demo")
	if err == nil || strings.Contains(out+err.Error(), secret) {
		t.Fatalf("API secret leaked: out=%q err=%v", out, err)
	}
}
