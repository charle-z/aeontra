package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func configuredPlatformService(t *testing.T, mode config.Mode, baseURL string) *Service {
	t.Helper()
	svc, _ := newTestService(t, mode)
	svc.WithGitHub(NewGitHubClient("https://api.github.test", "github-token", "acme", "org", "private"))
	svc.WithCoolify(NewCoolifyClient(baseURL, "coolify-token", nil).
		WithBuilderConfig("server1", "project1", "production", "", []string{"example.com"}))
	return svc
}

func TestPlatformAppsListAndStatusReturnSafeSummaries(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/applications" {
			_, _ = w.Write([]byte(`[{"uuid":"app1","name":"demo","status":"running","git_repository":"acme/demo","git_branch":"main","fqdn":"https://demo.example.com"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"uuid":"app1","name":"demo","status":"running","deployment_status":"finished","git_repository":"acme/demo","git_branch":"main","fqdn":"https://demo.example.com"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeReadOnly, ts.URL)
	for _, call := range []func() (string, error){svc.PlatformAppsList, func() (string, error) { return svc.PlatformAppStatus("app1") }} {
		out, err := call()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"uuid: app1", "name: demo", "repository: acme/demo", "branch: main", "demo.example.com"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary missing %q:\n%s", want, out)
			}
		}
	}
}

func TestPlatformAppLogsReturnsBoundedRedactedLogs(t *testing.T) {
	var gotLines string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/applications/app1/logs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotLines = r.URL.Query().Get("lines")
		_, _ = w.Write([]byte(`{"logs":"build started\ncoolify-token\nbuild failed"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeReadOnly, ts.URL)
	out, err := svc.PlatformAppLogs("app1", 25)
	if err != nil {
		t.Fatal(err)
	}
	if gotLines != "25" || !strings.Contains(out, "build started") || !strings.Contains(out, "build failed") {
		t.Fatalf("unexpected logs response lines=%q out=%q", gotLines, out)
	}
	if strings.Contains(out, "coolify-token") {
		t.Fatalf("Coolify token leaked in logs: %q", out)
	}
	if _, err := svc.PlatformAppLogs("app1", 1001); err == nil {
		t.Fatal("oversized log request accepted")
	}
}

func TestPlatformAppCreatePlannedSuccess(t *testing.T) {
	created := 0
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		created++
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uuid":"app1","name":"demo","status":"starting","git_repository":"acme/demo","git_branch":"main","fqdn":"https://demo.example.com"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeAsk, ts.URL)
	preview, err := svc.PlatformAppCreatePreview(PlatformAppCreateRequest{
		Name: "demo", GitHubRepo: "acme/demo", Branch: "main", Domain: "https://demo.example.com",
		Port: "3000", BuildPack: "nixpacks", HealthcheckPath: "/healthz", HealthcheckInterval: 15,
		RequiredEnv: []string{"DATABASE_URL", "API_KEY"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || strings.Contains(preview, "coolify-token") || !strings.Contains(preview, "required_environment_variables: API_KEY,DATABASE_URL") {
		t.Fatalf("unsafe preview created=%d:\n%s", created, preview)
	}
	planID := field(preview, "plan_id")
	out, err := svc.PlatformAppCreate(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || created != 0 {
		t.Fatalf("approval gate: out=%q err=%v created=%d", out, err, created)
	}
	out, err = svc.PlatformAppCreate(planID, true)
	if err != nil || !strings.Contains(out, "uuid: app1") || created != 1 {
		t.Fatalf("create: out=%q err=%v created=%d", out, err, created)
	}
	if payload["server_uuid"] != "server1" || payload["git_repository"] != "https://github.com/acme/demo.git" || payload["health_check_path"] != "/healthz" {
		t.Fatalf("bad payload: %#v", payload)
	}
	if _, err := svc.PlatformAppCreate(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replay must fail: %v", err)
	}
}

func TestPlatformAppCreateCanonicalizesRepositoryAndDefaultsStaticPort(t *testing.T) {
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uuid":"app1","name":"static-site"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeAllow, ts.URL)
	preview, err := svc.PlatformAppCreatePreview(PlatformAppCreateRequest{
		Name: "static-site", GitHubRepo: "acme/static-site", BuildPack: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "repository: https://github.com/acme/static-site.git") || !strings.Contains(preview, "port: 80") {
		t.Fatalf("preview did not canonicalize the Coolify payload:\n%s", preview)
	}
	if _, err := svc.PlatformAppCreate(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	if payload["git_repository"] != "https://github.com/acme/static-site.git" || payload["ports_exposes"] != "80" {
		t.Fatalf("bad static payload: %#v", payload)
	}
}

func TestPlatformAppCreateUsesConfiguredGitHubAppSource(t *testing.T) {
	var gotPath string
	var payload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uuid":"app1","name":"private-site"}`))
	}))
	defer ts.Close()

	svc := configuredPlatformService(t, config.ModeAllow, ts.URL)
	svc.WithCoolify(svc.coolify.WithGitHubApp("githubapp1"))
	preview, err := svc.PlatformAppCreatePreview(PlatformAppCreateRequest{
		Name: "private-site", GitHubRepo: "acme/private-site", Branch: "main", BuildPack: "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "source_type: private_github_app") {
		t.Fatalf("preview did not identify private GitHub App source:\n%s", preview)
	}
	if _, err := svc.PlatformAppCreate(field(preview, "plan_id"), true); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/applications/private-github-app" || payload["github_app_uuid"] != "githubapp1" {
		t.Fatalf("private GitHub App request path=%q payload=%#v", gotPath, payload)
	}
}

func TestPlatformAppCreateRejectsConfigurationDomainRepoAndExpiry(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	if _, err := svc.PlatformAppCreatePreview(PlatformAppCreateRequest{Name: "demo", GitHubRepo: "acme/demo"}); err == nil || !strings.Contains(err.Error(), "COOLIFY_URL") {
		t.Fatalf("missing config not explicit: %v", err)
	}

	svc = configuredPlatformService(t, config.ModeAllow, "https://coolify.test")
	for _, req := range []PlatformAppCreateRequest{
		{Name: "demo", GitHubRepo: "other/demo"},
		{Name: "demo", GitHubRepo: "acme/demo", Domain: "https://evil.test"},
		{Name: "demo", GitHubRepo: "acme/demo", Port: "--privileged"},
		{Name: "demo", GitHubRepo: "acme/demo", RequiredEnv: []string{"BAD=VALUE"}},
	} {
		if _, err := svc.PlatformAppCreatePreview(req); err == nil {
			t.Fatalf("unsafe create request accepted: %#v", req)
		}
	}
	preview, err := svc.PlatformAppCreatePreview(PlatformAppCreateRequest{Name: "demo", GitHubRepo: "acme/demo"})
	if err != nil {
		t.Fatal(err)
	}
	planID := field(preview, "plan_id")
	svc.plans.mu.Lock()
	svc.plans.plans[planID].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.PlatformAppCreate(planID, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired create plan must fail: %v", err)
	}
}

func TestPlatformDeployPlannedSuccessAndChangedState(t *testing.T) {
	branch := "main"
	deploys := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/deploy" {
			deploys++
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep1","status":"queued"}`))
			return
		}
		_, _ = w.Write([]byte(`{"uuid":"app1","name":"demo","status":"running","git_repository":"acme/demo","git_branch":"` + branch + `","git_commit_sha":"abc123"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeAsk, ts.URL)
	preview, err := svc.PlatformDeployPreview("app1")
	if err != nil {
		t.Fatal(err)
	}
	planID := field(preview, "plan_id")
	if _, err := svc.PlatformDeploy(planID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlatformDeploy(planID, true); err != nil || deploys != 1 {
		t.Fatalf("deploy failed err=%v count=%d", err, deploys)
	}

	preview, _ = svc.PlatformDeployPreview("app1")
	branch = "changed"
	if _, err := svc.PlatformDeploy(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "application changed") {
		t.Fatalf("changed app must fail: %v", err)
	}
}

func TestPlatformDeployParsesWrappedCoolifyResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/deploy" {
			_, _ = w.Write([]byte("{\"deployments\":[{\"deployment_uuid\":\"dep-wrapped\",\"status\":\"queued\",\"message\":\"deployment queued\"}]}"))
			return
		}
		_, _ = w.Write([]byte("{\"uuid\":\"app1\",\"name\":\"demo\",\"status\":\"running\",\"git_repository\":\"acme/demo\",\"git_branch\":\"main\",\"git_commit_sha\":\"abc123\"}"))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeAllow, ts.URL)
	preview, err := svc.PlatformDeployPreview("app1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.PlatformDeploy(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"deployment_id: dep-wrapped", "status: queued", "message: deployment queued"} {
		if !strings.Contains(out, want) {
			t.Fatalf("wrapped deploy response missing %q:\n%s", want, out)
		}
	}
}

func TestPlatformDeploymentStatusReturnsSafeSummary(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("{\"deployment_uuid\":\"dep1\",\"application_name\":\"demo\",\"status\":\"finished\",\"commit\":\"abc123\",\"commit_message\":\"ship it\",\"created_at\":\"2026-07-12T10:00:00Z\",\"updated_at\":\"2026-07-12T10:01:00Z\",\"finished_at\":\"2026-07-12T10:01:00Z\",\"logs\":\"raw secret host=private.example code=topology_front_application_transport_connection_refused token=never-return\"}"))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeReadOnly, ts.URL)
	out, err := svc.PlatformDeploymentStatus("dep1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/deployments/dep1" {
		t.Fatalf("path=%q", gotPath)
	}
	for _, want := range []string{"deployment_id: dep1", "application: demo", "status: finished", "commit: abc123", "finished_at: 2026-07-12T10:01:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("deployment summary missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "safe_code: topology_front_application_transport_connection_refused") {
		t.Fatalf("closed deployment code missing:\n%s", out)
	}
	for _, forbidden := range []string{"raw secret", "private.example", "never-return", "logs:"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("deployment logs leaked %q:\n%s", forbidden, out)
		}
	}
	if _, err := svc.PlatformDeploymentStatus("../unsafe"); err == nil {
		t.Fatal("unsafe deployment id accepted")
	}
}

func TestSafeCoordinatorDeploymentCodeRejectsUnknownOrAmbiguousLogs(t *testing.T) {
	t.Parallel()
	if got := safeCoordinatorDeploymentCode("code=topology_front_application_transport_unreviewed"); got != "" {
		t.Fatalf("unknown code accepted: %q", got)
	}
	logs := strings.Join([]string{
		"code=topology_front_application_transport_connection_refused",
		"code=topology_front_application_transport_route_unavailable",
	}, "\n")
	if got := safeCoordinatorDeploymentCode(logs); got != "" {
		t.Fatalf("ambiguous codes accepted: %q", got)
	}
	if got := safeCoordinatorDeploymentCode("secret only"); got != "" {
		t.Fatalf("arbitrary log accepted: %q", got)
	}
}

func TestPlatformAPIErrorsRedactToken(t *testing.T) {
	secret := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"` + secret + `"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeReadOnly, ts.URL)
	out, err := svc.PlatformAppsList()
	if err == nil || strings.Contains(out+err.Error(), secret) {
		t.Fatalf("API error leaked token: out=%q err=%v", out, err)
	}
}

func TestPlatformStatusOmitsCredentialURLs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"uuid":"app1","git_repository":"https://user:password@github.com/acme/demo.git","fqdn":"https://user:password@example.com"}`))
	}))
	defer ts.Close()
	svc := configuredPlatformService(t, config.ModeReadOnly, ts.URL)
	out, err := svc.PlatformAppStatus("app1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "password") || strings.Count(out, "[unsafe URL omitted]") != 2 {
		t.Fatalf("credential URLs were not omitted: %s", out)
	}
}
