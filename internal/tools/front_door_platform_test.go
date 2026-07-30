package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

const (
	frontDoorTestSHA     = "0123456789abcdef0123456789abcdef01234567"
	frontDoorTestCatalog = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func configuredFrontDoorPlatformService(t *testing.T, mode config.Mode, baseURL string) *Service {
	t.Helper()
	svc := configuredPlatformService(t, mode, baseURL)
	svc.WithGitHub(NewGitHubClient(baseURL, "github-token", "acme", "org", "private"))
	svc.WithCoolify(svc.coolify.WithBuilderRuntime("destination1", nil))
	return svc
}

func TestPlatformFrontDoorCreateIsFixedPlannedAndDeploysAfterEnvironment(t *testing.T) {
	created := 0
	domainPatches := 0
	envs := 0
	deploys := 0
	var payload map[string]any
	var domainPayload map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/private-github-app":
			created++
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1":
			domainPatches++
			if err := json.NewDecoder(r.Body).Decode(&domainPayload); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"uuid":"front1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/front1/envs":
			envs++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deploys++
			if r.URL.Query().Get("uuid") != "front1" || r.URL.Query().Get("force") != "false" {
				t.Fatalf("unsafe deploy query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"deployment_uuid":"dep1","status":"queued"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredFrontDoorPlatformService(t, config.ModeAsk, ts.URL)
	svc.WithCoolify(svc.coolify.WithGitHubApp("githubapp1"))
	request := PlatformFrontDoorRequest{
		Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com",
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	}
	preview, err := svc.PlatformFrontDoorCreatePreview(request)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || deploys != 0 || !strings.Contains(preview, "action: create") ||
		!strings.Contains(preview, "branch_sha: "+frontDoorTestSHA) || strings.Contains(preview, "github-token") {
		t.Fatalf("unsafe preview created=%d deploys=%d:\n%s", created, deploys, preview)
	}
	plan := field(preview, "plan_id")
	out, err := svc.PlatformFrontDoorCreate(plan, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || created != 0 {
		t.Fatalf("approval gate out=%q err=%v created=%d", out, err, created)
	}
	out, err = svc.PlatformFrontDoorCreate(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || domainPatches != 1 || envs != 3 || deploys != 1 || !strings.Contains(out, "application_uuid: front1") || !strings.Contains(out, "deployment_id: dep1") {
		t.Fatalf("created=%d domainPatches=%d envs=%d deploys=%d out=%s", created, domainPatches, envs, deploys, out)
	}
	if _, exists := payload["fqdn"]; exists {
		t.Fatalf("private GitHub App creation payload unexpectedly contains fqdn: %#v", payload)
	}
	if domainPayload["fqdn"] != "https://front-door.example.com" {
		t.Fatalf("domain patch payload=%#v", domainPayload)
	}
	for key, want := range map[string]any{
		"name":                   "mcp-devbox-front-door-managed",
		"github_app_uuid":        "githubapp1",
		"git_branch":             "front-door-stable",
		"destination_uuid":       "destination1",
		"build_pack":             "dockerfile",
		"dockerfile_location":    "/Dockerfile.front-door",
		"ports_exposes":          "8765",
		"ports_mappings":         "",
		"autogenerate_domain":    false,
		"is_auto_deploy_enabled": false,
		"instant_deploy":         false,
		"health_check_path":      "/front-door/healthz",
	} {
		if payload[key] != want {
			t.Fatalf("payload[%s]=%#v want %#v; full=%#v", key, payload[key], want, payload)
		}
	}
	if options, _ := payload["custom_docker_run_options"].(string); options != "" {
		t.Fatalf("front door received docker options: %q", options)
	}
}

func TestPlatformFrontDoorCreateReconcilesOneExistingAppAndSkipsDuplicateDeploy(t *testing.T) {
	created := 0
	patched := 0
	deploys := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","git_repository":"acme/mcp-devbox","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://front-door.example.com","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte(`[{"uuid":"env1","key":"MCP_FRONT_DOOR_BACKEND_URL"},{"uuid":"env2","key":"MCP_FRONT_DOOR_EXPECTED_PROTOCOL"},{"uuid":"env3","key":"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1/envs":
			patched++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"uuid":"env1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/public":
			created++
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deploys++
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredFrontDoorPlatformService(t, config.ModeAllow, ts.URL)
	request := PlatformFrontDoorRequest{
		Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com",
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	}
	preview, err := svc.PlatformFrontDoorCreatePreview(request)
	if err != nil || !strings.Contains(preview, "action: reconcile") {
		t.Fatalf("preview=%s err=%v", preview, err)
	}
	out, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || patched != 3 || deploys != 0 || !strings.Contains(out, "deployment_skipped: already_serving_expected_commit") {
		t.Fatalf("created=%d patched=%d deploys=%d out=%s", created, patched, deploys, out)
	}
}

func TestPlatformFrontDoorRejectsUnsafeTopologyAndMismatchedExistingApp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			_, _ = w.Write([]byte(`{"object":{"sha":"` + frontDoorTestSHA + `"}}`))
		case r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","git_repository":"https://github.com/acme/other.git","git_branch":"front-door-stable"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()
	svc := configuredFrontDoorPlatformService(t, config.ModeAllow, ts.URL)

	for _, request := range []PlatformFrontDoorRequest{
		{Domain: "https://evil.test", BackendURL: "https://mcp-backend.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
		{Domain: "https://front-door.example.com", BackendURL: "https://front-door.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
		{Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com/path", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
		{Domain: "https://front-door.example.com", BackendURL: "http://mcp-backend.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
		{Domain: "https://front-door.example.com:8443", BackendURL: "https://mcp-backend.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
		{Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com:443", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog},
	} {
		if _, err := svc.PlatformFrontDoorCreatePreview(request); err == nil {
			t.Fatalf("unsafe request accepted: %+v", request)
		}
	}
	request := PlatformFrontDoorRequest{Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog}
	if _, err := svc.PlatformFrontDoorCreatePreview(request); err == nil || !strings.Contains(err.Error(), "does not match the managed contract") {
		t.Fatalf("mismatched existing app accepted: %v", err)
	}
}

func TestPlatformFrontDoorRequiresConfiguredDomainAllowlist(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected external request %s %s", r.Method, r.URL.String())
	}))
	defer ts.Close()

	svc := configuredFrontDoorPlatformService(t, config.ModeAllow, ts.URL)
	svc.WithCoolify(NewCoolifyClient(ts.URL, "coolify-token", nil).
		WithBuilderConfig("server1", "project1", "production", "", nil).
		WithBuilderRuntime("destination1", nil))
	_, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com",
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err == nil || !strings.Contains(err.Error(), "COOLIFY_ALLOWED_DOMAINS is required") {
		t.Fatalf("missing domain allowlist accepted: %v", err)
	}
}

func TestPlatformFrontDoorCreateRejectsStableBranchChangeAfterPreview(t *testing.T) {
	branchReads := 0
	created := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			branchReads++
			sha := frontDoorTestSHA
			if branchReads > 1 {
				sha = "fedcba9876543210fedcba9876543210fedcba98"
			}
			_, _ = w.Write([]byte("{\"object\":{\"sha\":\"" + sha + "\"}}"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/public":
			created++
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredFrontDoorPlatformService(t, config.ModeAllow, ts.URL)
	preview, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com",
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err == nil || !strings.Contains(err.Error(), "stable front-door branch changed") || created != 0 {
		t.Fatalf("err=%v created=%d", err, created)
	}
}

func TestPlatformFrontDoorCreateRecoversPartialApplicationWithoutDomain(t *testing.T) {
	created := 0
	domainPatches := 0
	envs := 0
	deploys := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/mcp-devbox/git/ref/heads/front-door-stable":
			_, _ = w.Write([]byte("{\"object\":{\"sha\":\"" + frontDoorTestSHA + "\"}}"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_, _ = w.Write([]byte("[{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\"}]"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1":
			_, _ = w.Write([]byte("{\"uuid\":\"front1\",\"name\":\"mcp-devbox-front-door-managed\",\"status\":\"exited:unknown\",\"git_repository\":\"https://github.com/acme/mcp-devbox.git\",\"git_branch\":\"front-door-stable\",\"git_commit_sha\":\"" + frontDoorTestSHA + "\",\"build_pack\":\"dockerfile\",\"dockerfile_location\":\"/Dockerfile.front-door\",\"ports_exposes\":\"8765\",\"is_auto_deploy_enabled\":false,\"instant_deploy\":false,\"health_check_path\":\"/front-door/healthz\"}"))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/front1":
			domainPatches++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{\"uuid\":\"front1\"}"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/front1/envs":
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/front1/envs":
			envs++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{\"uuid\":\"env1\"}"))
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/applications/public" || r.URL.Path == "/api/v1/applications/private-github-app"):
			created++
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/deploy":
			deploys++
			_, _ = w.Write([]byte("{\"deployment_uuid\":\"dep-recovered\",\"status\":\"queued\"}"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	svc := configuredFrontDoorPlatformService(t, config.ModeAllow, ts.URL)
	preview, err := svc.PlatformFrontDoorCreatePreview(PlatformFrontDoorRequest{
		Domain: "https://front-door.example.com", BackendURL: "https://mcp-backend.example.com",
		ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: frontDoorTestCatalog,
	})
	if err != nil || !strings.Contains(preview, "action: reconcile") {
		t.Fatalf("preview=%s err=%v", preview, err)
	}
	out, err := svc.PlatformFrontDoorCreate(field(preview, "plan_id"), true)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || domainPatches != 1 || envs != 3 || deploys != 1 || !strings.Contains(out, "deployment_id: dep-recovered") {
		t.Fatalf("created=%d domainPatches=%d envs=%d deploys=%d out=%s", created, domainPatches, envs, deploys, out)
	}
}

func TestPlatformFrontDoorStatusIsFixedAndDoesNotRequireApplicationAllowlist(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/applications":
			_, _ = w.Write([]byte(`[{"uuid":"front1","name":"mcp-devbox-front-door-managed"}]`))
		case "/api/v1/applications/front1":
			_, _ = w.Write([]byte(`{"uuid":"front1","name":"mcp-devbox-front-door-managed","status":"running:healthy","deployment_status":"finished","git_repository":"https://github.com/acme/mcp-devbox.git","git_branch":"front-door-stable","git_commit_sha":"` + frontDoorTestSHA + `","fqdn":"https://front-door.example.com","build_pack":"dockerfile","dockerfile_location":"/Dockerfile.front-door","ports_exposes":"8765","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/front-door/healthz"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()
	svc := configuredFrontDoorPlatformService(t, config.ModeReadOnly, ts.URL)
	svc.WithCoolify(NewCoolifyClient(ts.URL, "coolify-token", []string{"different-app"}).WithBuilderConfig("server1", "project1", "production", "", []string{"example.com"}).WithBuilderRuntime("destination1", nil))
	out, err := svc.PlatformFrontDoorStatus()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"application_uuid: front1", "status: running:healthy", "contract: valid", "branch: front-door-stable", "commit: " + frontDoorTestSHA} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q:\n%s", want, out)
		}
	}
}
