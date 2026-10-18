package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func fakeCoolify(t *testing.T, baseURL, token string, allowed []string, do func(*http.Request) (*http.Response, error)) *CoolifyClient {
	t.Helper()
	c := NewCoolifyClient(baseURL, token, allowed)
	if do != nil {
		c.do = do
	}
	return c
}

func TestCoolifyDeploy_NotConfigured(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow) // no coolify client set
	if _, err := svc.CoolifyDeploy("app123", true); err == nil {
		t.Error("coolify_deploy must be denied when not configured")
	}
	// Configured with empty token is still "not configured".
	svc.WithCoolify(NewCoolifyClient("https://c.example.com", "", nil))
	if _, err := svc.CoolifyDeploy("app123", true); err == nil {
		t.Error("coolify_deploy must be denied without a token")
	}
}

func TestCoolifyDeploy_RejectsUnsafeAppID(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	called := false
	svc.WithCoolify(fakeCoolify(t, "https://c.example.com", "tok", nil, func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	for _, bad := range []string{"", "app/../x", "http://evil", "app?x=1", "a b", "a&b"} {
		if _, err := svc.CoolifyDeploy(bad, true); err == nil {
			t.Errorf("unsafe app id %q must be rejected", bad)
		}
	}
	if called {
		t.Error("no HTTP request should be made for an invalid app id (SSRF guard)")
	}
}

func TestCoolifyDeploy_AllowlistEnforced(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithCoolify(fakeCoolify(t, "https://c.example.com", "tok", []string{"allowedapp"}, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	if _, err := svc.CoolifyDeploy("otherapp", true); err == nil {
		t.Error("app not in allowlist must be denied")
	}
}

func TestCoolifyDeploy_ReadOnlyDeniedAndAskApproval(t *testing.T) {
	ro, _ := newTestService(t, config.ModeReadOnly)
	ro.WithCoolify(fakeCoolify(t, "https://c.example.com", "tok", nil, nil))
	if _, err := ro.CoolifyDeploy("app1", true); err == nil {
		t.Error("read-only must deny coolify_deploy")
	}

	ask, _ := newTestService(t, config.ModeAsk)
	called := false
	ask.WithCoolify(fakeCoolify(t, "https://c.example.com", "tok", nil, func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}))
	msg, err := ask.CoolifyDeploy("app1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") || called {
		t.Errorf("ask mode must require approval and make no call: msg=%q called=%v", msg, called)
	}
}

func TestCoolifyDeploy_AllowSendsTokenInHeaderOnly(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	const token = "SUPER-SECRET-COOLIFY-TOKEN"
	var gotURL, gotAuth string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com/", token, nil, func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"message":"deploy queued"}`))}, nil
	}))
	out, err := svc.CoolifyDeploy("app123abc", false)
	if err != nil {
		t.Fatal(err)
	}
	// URL targets the configured base + deploy endpoint with the uuid, NOT the token.
	if !strings.Contains(gotURL, "/api/v1/deploy") || !strings.Contains(gotURL, "uuid=app123abc") {
		t.Errorf("unexpected deploy URL: %s", gotURL)
	}
	if strings.Contains(gotURL, token) {
		t.Errorf("token must NEVER appear in the URL: %s", gotURL)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("token must ride in the Authorization header, got %q", gotAuth)
	}
	// The token must never be returned to the agent.
	if strings.Contains(out, token) {
		t.Errorf("token leaked into tool output: %s", out)
	}
	if !strings.Contains(out, "HTTP 200") || !strings.Contains(out, "deploy queued") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestCoolifyListApps_ReadsApplications(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	var gotPath string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"uuid":"app1","name":"demo"}]`))}, nil
	}))

	out, err := svc.CoolifyListApps()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/applications" {
		t.Fatalf("path=%q, want /api/v1/applications", gotPath)
	}
	if !strings.Contains(out, "app1") || !strings.Contains(out, "demo") {
		t.Fatalf("unexpected list output: %q", out)
	}
}

func TestCoolifyAppStatus_ReadsApplication(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	var gotPath string
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"uuid":"app1","name":"demo","fqdn":"https://demo.example.com"}`))}, nil
	}))

	out, err := svc.CoolifyAppStatus("app1")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/applications/app1" {
		t.Fatalf("path=%q, want /api/v1/applications/app1", gotPath)
	}
	if !strings.Contains(out, "demo.example.com") {
		t.Fatalf("unexpected status output: %q", out)
	}
}

func TestCoolifyCreateApp_AskRequiresApproval(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	called := false
	c := fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}).WithBuilderConfig("server1", "project1", "production", "", nil)
	svc.WithCoolify(c)

	out, err := svc.CoolifyCreateApp("demo", "charle-z/demo", "main", "nixpacks", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "APPROVAL REQUIRED") || called {
		t.Fatalf("ask mode should require approval before API call, out=%q called=%v", out, called)
	}
}

func TestCoolifyCreateApp_SendsConfiguredPayload(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	var gotPath string
	var gotBody map[string]any
	c := fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"uuid":"app1","name":"demo","fqdn":"https://demo.example.com"}`))}, nil
	}).WithBuilderConfig("server1", "project1", "production", "", []string{"example.com"})
	svc.WithCoolify(c)

	out, err := svc.CoolifyCreateApp("demo", "charle-z/demo", "main", "nixpacks", "3000", "https://demo.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/applications/public" {
		t.Fatalf("path=%q, want /api/v1/applications/public", gotPath)
	}
	if gotBody["server_uuid"] != "server1" || gotBody["project_uuid"] != "project1" ||
		gotBody["environment_name"] != "production" || gotBody["git_repository"] != "charle-z/demo" ||
		gotBody["git_branch"] != "main" || gotBody["build_pack"] != "nixpacks" ||
		gotBody["ports_exposes"] != "3000" || gotBody["fqdn"] != "https://demo.example.com" {
		t.Fatalf("bad create payload: %#v", gotBody)
	}
	if !strings.Contains(out, "app1") {
		t.Fatalf("unexpected create output: %q", out)
	}
}

func TestCoolifyCreateApp_DeniesDomainOutsideAllowlist(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	c := fakeCoolify(t, "https://coolify.example.com", "tok", nil, nil).
		WithBuilderConfig("server1", "project1", "production", "", []string{"example.com"})
	svc.WithCoolify(c)

	if _, err := svc.CoolifyCreateApp("demo", "charle-z/demo", "main", "nixpacks", "", "https://evil.test", true); err == nil {
		t.Fatal("domain outside COOLIFY_ALLOWED_DOMAINS should be denied")
	}
}

func TestCoolifySetEnv_RedactsValues(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	var gotPath string
	var gotBody map[string]any
	svc.WithCoolify(fakeCoolify(t, "https://coolify.example.com", "tok", nil, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`))}, nil
		}
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(`{"key":"API_KEY","value":"ghp_0123456789abcdefghijklmnopqrstuvwxyz"}`))}, nil
	}))

	out, err := svc.CoolifySetEnv("app1", map[string]string{"API_KEY": "ghp_0123456789abcdefghijklmnopqrstuvwxyz"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/applications/app1/envs" || gotBody["key"] != "API_KEY" || gotBody["value"] == "" {
		t.Fatalf("bad env request path=%q body=%#v", gotPath, gotBody)
	}
	if strings.Contains(out, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("env output leaked secret: %q", out)
	}
}
