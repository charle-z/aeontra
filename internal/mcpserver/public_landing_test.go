package mcpserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestPublicLandingIsUnauthenticatedWithoutChangingPrivateRoutes(t *testing.T) {
	handler, _ := newHTTPServer(t, config.ModeReadOnly)

	landing := do(t, handler, http.MethodGet, "/", "", "")
	if landing.Code != http.StatusOK {
		t.Fatalf("GET / status=%d body=%s", landing.Code, landing.Body.String())
	}
	for _, required := range []string{"Aeontra", "presentation-only", "/landing/assets/app.css"} {
		if !strings.Contains(landing.Body.String(), required) {
			t.Errorf("GET / missing %q", required)
		}
	}

	mcp := do(t, handler, http.MethodPost, DefaultMCPPath, "", `{}`)
	if mcp.Code != http.StatusUnauthorized {
		t.Fatalf("public landing changed MCP auth: status=%d", mcp.Code)
	}

	console := do(t, handler, http.MethodGet, "/console", "", "")
	if console.Code != http.StatusOK || !strings.Contains(strings.ToLower(console.Body.String()), "sign in") {
		t.Fatalf("public landing changed console login: status=%d body=%s", console.Code, console.Body.String())
	}

	version := do(t, handler, http.MethodGet, "/version", "", "")
	if version.Code != http.StatusOK || !strings.Contains(version.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("public landing changed /version: status=%d type=%q", version.Code, version.Header().Get("Content-Type"))
	}
}

func TestPublicLandingRejectsWritesAndHardensUnknownRoutes(t *testing.T) {
	handler, _ := newHTTPServer(t, config.ModeReadOnly)

	post := do(t, handler, http.MethodPost, "/", "", "")
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST / status=%d allow=%q", post.Code, post.Header().Get("Allow"))
	}

	missing := do(t, handler, http.MethodGet, "/not-a-public-control-plane", "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", missing.Code)
	}
	if !strings.Contains(missing.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("unknown route lacks safe CSP")
	}
}
