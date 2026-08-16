package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

const platformDomainTestCommit = "21ef0f6b173962185ea1e86b99cc6c0e61aa9f8e"

func platformDomainApp(domain, branch, status string) string {
	return `{"uuid":"app1","name":"demo","status":"` + status + `","deployment_status":"finished","git_repository":"acme/demo","git_branch":"` + branch + `","git_commit_sha":"` + platformDomainTestCommit + `","fqdn":"` + domain + `","build_pack":"dockerfile","dockerfile_location":"/Dockerfile","ports_exposes":"3000","is_auto_deploy_enabled":false,"instant_deploy":false,"health_check_path":"/health","destination_uuid":"destination1","custom_docker_run_options":"--read-only"}`
}

func platformDomainDeployment(status string) string {
	return `[{"deployment_uuid":"dep1","status":"` + status + `","commit":"` + platformDomainTestCommit + `","created_at":"2026-08-15T10:00:00Z","updated_at":"2026-08-15T10:01:00Z"}]`
}

func TestPlatformAppDomainUpdatePlansAndChangesOnlyDomain(t *testing.T) {
	currentDomain := "http://demo.example.com"
	patches := 0
	var patchBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp(currentDomain, "main", "running:healthy")))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment("finished")))
		case request.Method == http.MethodPatch && request.URL.Path == "/api/v1/applications/app1":
			patches++
			if err := json.NewDecoder(request.Body).Decode(&patchBody); err != nil {
				t.Fatal(err)
			}
			currentDomain = patchBody["domains"].(string)
			_, _ = response.Write([]byte(`{"uuid":"app1"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAsk, server.URL)
	preview, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"app: app1", "current_domain: http://demo.example.com",
		"target_domain: https://demo.example.com", "change_required: true", "effect: PATCH only the application domains field",
	} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	if patches != 0 {
		t.Fatalf("preview mutated the app: patches=%d", patches)
	}
	planID := field(preview, "plan_id")
	out, err := service.PlatformAppDomainUpdate(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || patches != 0 {
		t.Fatalf("approval gate out=%q err=%v patches=%d", out, err, patches)
	}
	out, err = service.PlatformAppDomainUpdate(planID, true)
	if err != nil {
		t.Fatal(err)
	}
	if patches != 1 || len(patchBody) != 2 || patchBody["domains"] != "https://demo.example.com" || patchBody["force_domain_override"] != false {
		t.Fatalf("domain patch was not narrow: patches=%d body=%#v", patches, patchBody)
	}
	for _, want := range []string{"changed: true", "domain: https://demo.example.com", "preserved_configuration: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("result missing %q:\n%s", want, out)
		}
	}
	if _, err := service.PlatformAppDomainUpdate(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("plan replay must fail: %v", err)
	}
}

func TestPlatformAppDomainUpdateRejectsUnsafeTargetsAndState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp("http://demo.example.com", "main", "running:healthy")))
		case "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment("finished")))
		default:
			t.Fatalf("unexpected request %s", request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	for _, domain := range []string{
		"http://demo.example.com",
		"https://user:pass@demo.example.com",
		"https://demo.example.com:8443",
		"https://demo.example.com/path",
		"https://demo.example.com?query=1",
		"https://demo.example.com#fragment",
		"https://evil.test",
		"https://127.0.0.1",
	} {
		if _, err := service.PlatformAppDomainUpdatePreview("app1", domain); err == nil {
			t.Fatalf("unsafe domain accepted: %s", domain)
		}
	}

	service.WithCoolify(NewCoolifyClient(server.URL, "coolify-token", nil).
		WithBuilderConfig("server1", "project1", "production", "", nil))
	if _, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com"); err == nil || !strings.Contains(err.Error(), "COOLIFY_ALLOWED_DOMAINS is required") {
		t.Fatalf("empty domain policy must fail closed: %v", err)
	}
}

func TestPlatformAppDomainUpdateRejectsActiveDeploymentAndChangedApp(t *testing.T) {
	branch := "main"
	deploymentStatus := "in_progress"
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp("http://demo.example.com", branch, "running:healthy")))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment(deploymentStatus)))
		case request.Method == http.MethodPatch:
			patches++
			_, _ = response.Write([]byte(`{"uuid":"app1"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	if _, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com"); err == nil || !strings.Contains(err.Error(), "latest deployment is not finished") {
		t.Fatalf("active deployment accepted: %v", err)
	}
	deploymentStatus = "finished"
	preview, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	branch = "changed"
	if _, err := service.PlatformAppDomainUpdate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "application changed after domain preview") {
		t.Fatalf("changed app accepted: %v", err)
	}
	if patches != 0 {
		t.Fatalf("changed app was patched: %d", patches)
	}
}

func TestPlatformAppDomainUpdateCompensatesUnexpectedConfigurationDrift(t *testing.T) {
	currentDomain := "http://demo.example.com"
	branch := "main"
	var domains []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp(currentDomain, branch, "running:healthy")))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment("finished")))
		case request.Method == http.MethodPatch && request.URL.Path == "/api/v1/applications/app1":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			domain := body["domains"].(string)
			domains = append(domains, domain)
			currentDomain = domain
			if len(domains) == 1 {
				branch = "unexpected"
			} else {
				branch = "main"
			}
			_, _ = response.Write([]byte(`{"uuid":"app1"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	preview, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlatformAppDomainUpdate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "compensated") {
		t.Fatalf("configuration drift was not compensated: %v", err)
	}
	if len(domains) != 2 || domains[0] != "https://demo.example.com" || domains[1] != "http://demo.example.com" {
		t.Fatalf("unexpected compensation sequence: %#v", domains)
	}
}

func TestPlatformAppDomainUpdateCompensatesAmbiguousPatchFailure(t *testing.T) {
	currentDomain := "http://demo.example.com"
	var domains []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp(currentDomain, "main", "running:healthy")))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment("finished")))
		case request.Method == http.MethodPatch && request.URL.Path == "/api/v1/applications/app1":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			domain := body["domains"].(string)
			domains = append(domains, domain)
			currentDomain = domain
			if len(domains) == 1 {
				http.Error(response, "upstream response lost", http.StatusBadGateway)
				return
			}
			_, _ = response.Write([]byte(`{"uuid":"app1"}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	preview, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PlatformAppDomainUpdate(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), "was compensated") {
		t.Fatalf("ambiguous mutation was not compensated: %v", err)
	}
	if len(domains) != 2 || domains[0] != "https://demo.example.com" || domains[1] != "http://demo.example.com" {
		t.Fatalf("unexpected compensation sequence: %#v", domains)
	}
}

func TestPlatformAppDomainUpdateIsNoopWhenAlreadyConfigured(t *testing.T) {
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/applications/app1":
			_, _ = response.Write([]byte(platformDomainApp("https://demo.example.com", "main", "running:healthy")))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/deployments/applications/app1":
			_, _ = response.Write([]byte(platformDomainDeployment("finished")))
		case request.Method == http.MethodPatch:
			patches++
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	service := configuredPlatformService(t, config.ModeAllow, server.URL)
	preview, err := service.PlatformAppDomainUpdatePreview("app1", "https://demo.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "change_required: false") {
		t.Fatalf("no-op preview not identified:\n%s", preview)
	}
	out, err := service.PlatformAppDomainUpdate(field(preview, "plan_id"), true)
	if err != nil || !strings.Contains(out, "changed: false") || patches != 0 {
		t.Fatalf("no-op execution out=%q err=%v patches=%d", out, err, patches)
	}
}
