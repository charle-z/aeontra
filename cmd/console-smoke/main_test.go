package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

func TestRunValidatesAuthenticatedConsoleWithoutPrintingSecrets(t *testing.T) {
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	const token = "production-like-console-smoke-token"
	handler, err := console.New(console.Config{
		StaticToken: token,
		Runtime: console.Status{
			Status:          local.Status,
			Version:         local.Version,
			ProtocolVersion: local.ProtocolVersion,
			Commit:          "expected-console-sha",
			ToolCount:       local.ToolCount,
			CatalogHash:     local.CatalogHash,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(withRuntimeHeadersForSmoke(mux, "expected-console-sha", local.ToolCount, local.CatalogHash))
	defer server.Close()

	var output bytes.Buffer
	if err := run(
		[]string{"--url", server.URL, "--expected-commit", "expected-console-sha"},
		&output,
		func(name string) string {
			if name == consoleTokenEnv {
				return token
			}
			return ""
		},
		server.Client(),
	); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"console smoke passed", "expected-console-sha", "tool_count=" + strconv.Itoa(local.ToolCount), local.CatalogHash, "surface=presentation-only"} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("output missing %q: %s", required, output.String())
		}
	}
	if strings.Contains(output.String(), token) || strings.Contains(output.String(), "mcpdevbox_console") {
		t.Fatalf("output leaked secret/session metadata: %s", output.String())
	}
}

func TestRunRejectsMissingTokenAndCommitMismatch(t *testing.T) {
	if err := run([]string{"--url", "https://example.test", "--expected-commit", "sha"}, &bytes.Buffer{}, func(string) string { return "" }, nil); err == nil || !strings.Contains(err.Error(), consoleTokenEnv) {
		t.Fatalf("missing token error=%v", err)
	}

	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	const token = "console-token"
	handler, err := console.New(console.Config{
		StaticToken: token,
		Runtime:     console.Status{Status: local.Status, Version: local.Version, ProtocolVersion: local.ProtocolVersion, Commit: "old-sha", ToolCount: local.ToolCount, CatalogHash: local.CatalogHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(withRuntimeHeadersForSmoke(mux, "old-sha", local.ToolCount, local.CatalogHash))
	defer server.Close()

	err = run([]string{"--url", server.URL, "--expected-commit", "new-sha"}, &bytes.Buffer{}, func(string) string { return token }, server.Client())
	if err == nil || !strings.Contains(err.Error(), "console commit") {
		t.Fatalf("commit mismatch error=%v", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestRunRejectsUnknownStatusFieldsAndWeakHeaders(t *testing.T) {
	const token = "console-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/console/login":
			setSmokeHeaders(w)
			http.SetCookie(w, &http.Cookie{Name: "mcpdevbox_console", Value: "opaque-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.Header().Set("Location", "/console")
			w.WriteHeader(http.StatusSeeOther)
		case "/console/status":
			setSmokeHeaders(w)
			w.Header().Set("X-MCP-Server-Commit", "sha")
			w.Header().Set("X-MCP-Tool-Count", "62")
			w.Header().Set("X-MCP-Catalog-Hash", "sha256:"+strings.Repeat("a", 64))
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.2.0","protocol_version":"2024-11-05","commit":"sha","tool_count":62,"catalog_hash":"sha256:` + strings.Repeat("a", 64) + `","authenticated":true,"surface":"presentation-only","private_path":"/secret"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := run([]string{"--url", server.URL, "--expected-commit", "sha"}, &bytes.Buffer{}, func(string) string { return token }, server.Client())
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}

	weak := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/console/login" {
			w.Header().Set("Location", "/console")
			http.SetCookie(w, &http.Cookie{Name: "mcpdevbox_console", Value: "opaque", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.WriteHeader(http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
	}))
	defer weak.Close()
	err = run([]string{"--url", weak.URL, "--expected-commit", "sha"}, &bytes.Buffer{}, func(string) string { return token }, weak.Client())
	if err == nil || !strings.Contains(err.Error(), "headers") {
		t.Fatalf("weak header error=%v", err)
	}
}

func TestConsoleEndpointValidation(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://example.com",
		"https://user:pass@example.com",
		"https://example.com?token=x",
		"https://example.com/private",
	} {
		if _, err := consoleEndpoint(raw); err == nil {
			t.Fatalf("consoleEndpoint(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"https://example.com", "https://example.com/console", "http://127.0.0.1:8765/console"} {
		endpoint, err := consoleEndpoint(raw)
		if err != nil || endpoint.Path != "/console" {
			t.Fatalf("consoleEndpoint(%q)=%v err=%v", raw, endpoint, err)
		}
	}
}

func withRuntimeHeadersForSmoke(next http.Handler, commit string, toolCount int, catalogHash string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-MCP-Server-Commit", commit)
		w.Header().Set("X-MCP-Tool-Count", strconv.Itoa(toolCount))
		w.Header().Set("X-MCP-Catalog-Hash", catalogHash)
		next.ServeHTTP(w, r)
	})
}

func setSmokeHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=()")
	w.Header().Set("Cache-Control", "no-store")
}
