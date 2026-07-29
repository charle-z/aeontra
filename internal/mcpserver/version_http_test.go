package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestVersionEndpointReportsSafeLiveCatalogIdentity(t *testing.T) {
	oldCommit, oldBuiltAt := buildinfo.Commit, buildinfo.BuiltAt
	buildinfo.Commit = "catalog-commit"
	buildinfo.BuiltAt = "2026-07-12T22:30:00Z"
	defer func() {
		buildinfo.Commit = oldCommit
		buildinfo.BuiltAt = oldBuiltAt
	}()

	h, _ := newHTTPServer(t, config.ModeReadOnly)
	rr := do(t, h, http.MethodGet, "/version", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /version = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Status          string `json:"status"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocol_version"`
		Commit          string `json:"commit"`
		BuiltAt         string `json:"built_at"`
		ToolCount       int    `json:"tool_count"`
		CatalogHash     string `json:"catalog_hash"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("version response is not JSON: %v\n%s", err, rr.Body.String())
	}
	if got.Status != "ok" || got.Version != buildinfo.Version || got.ProtocolVersion != buildinfo.ProtocolVersion {
		t.Fatalf("unexpected runtime identity: %#v", got)
	}
	if got.Commit != "catalog-commit" || got.BuiltAt != "2026-07-12T22:30:00Z" {
		t.Fatalf("unexpected build stamp: %#v", got)
	}
	if got.ToolCount == 0 || !strings.HasPrefix(got.CatalogHash, "sha256:") {
		t.Fatalf("missing catalog identity: %#v", got)
	}
	for _, forbidden := range []string{"token", "secret", "password", "/repos", "coolify", "github"} {
		if strings.Contains(strings.ToLower(rr.Body.String()), forbidden) {
			t.Fatalf("version response contains forbidden detail %q: %s", forbidden, rr.Body.String())
		}
	}
	if rr.Header().Get("X-MCP-Server-Commit") != got.Commit {
		t.Fatalf("commit header = %q, body = %q", rr.Header().Get("X-MCP-Server-Commit"), got.Commit)
	}
	if rr.Header().Get("X-MCP-Catalog-Hash") != got.CatalogHash {
		t.Fatalf("catalog header = %q, body = %q", rr.Header().Get("X-MCP-Catalog-Hash"), got.CatalogHash)
	}
}

func TestDynamicHTTPResponsesDisableCaching(t *testing.T) {
	h, _ := newHTTPServer(t, config.ModeReadOnly)
	tests := []struct {
		method string
		path   string
		auth   string
		body   string
	}{
		{method: http.MethodGet, path: "/healthz"},
		{method: http.MethodGet, path: "/version"},
		{method: http.MethodPost, path: "/mcp", auth: "Bearer " + testToken, body: `{"jsonrpc":"2.0","id":1,"method":"initialize"}`},
	}
	for _, test := range tests {
		rr := do(t, h, test.method, test.path, test.auth, test.body)
		cacheControl := strings.ToLower(rr.Header().Get("Cache-Control"))
		if !strings.Contains(cacheControl, "no-store") {
			t.Errorf("%s %s Cache-Control = %q, want no-store", test.method, test.path, cacheControl)
		}
		if strings.ToLower(rr.Header().Get("Pragma")) != "no-cache" {
			t.Errorf("%s %s Pragma = %q, want no-cache", test.method, test.path, rr.Header().Get("Pragma"))
		}
	}
}
