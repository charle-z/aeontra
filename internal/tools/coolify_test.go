package tools

import (
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
