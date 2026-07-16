package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

func TestRunAcceptsMatchingDeployedCatalog(t *testing.T) {
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	if local.ToolCount != 78 || local.CatalogHash != "sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed" {
		t.Fatalf("Step 6 catalog identity = %d %s", local.ToolCount, local.CatalogHash)
	}
	server := versionServer(t, versionResponse{
		Status:          "ok",
		Version:         local.Version,
		ProtocolVersion: local.ProtocolVersion,
		Commit:          "expected-sha",
		BuiltAt:         "2026-07-12T23:00:00Z",
		ToolCount:       local.ToolCount,
		CatalogHash:     local.CatalogHash,
	}, nil)
	defer server.Close()

	var output bytes.Buffer
	if err := run([]string{"--url", server.URL, "--expected-commit", "expected-sha"}, &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"catalog smoke passed", "commit=expected-sha", local.CatalogHash} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q: %s", expected, output.String())
		}
	}
}

func TestRunRejectsCommitAndCatalogMismatch(t *testing.T) {
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name           string
		remote         versionResponse
		expectedCommit string
		wantError      string
	}{
		{
			name: "commit",
			remote: versionResponse{Status: "ok", Version: local.Version, ProtocolVersion: local.ProtocolVersion,
				Commit: "old-sha", ToolCount: local.ToolCount, CatalogHash: local.CatalogHash},
			expectedCommit: "new-sha",
			wantError:      "deployed commit",
		},
		{
			name: "catalog",
			remote: versionResponse{Status: "ok", Version: local.Version, ProtocolVersion: local.ProtocolVersion,
				Commit: "expected-sha", ToolCount: local.ToolCount, CatalogHash: "sha256:" + strings.Repeat("0", 64)},
			expectedCommit: "expected-sha",
			wantError:      "catalog hash",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := versionServer(t, test.remote, nil)
			defer server.Close()
			err := run([]string{"--url", server.URL, "--expected-commit", test.expectedCommit}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestRunRejectsHeaderMismatchAndOversizedBody(t *testing.T) {
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	remote := versionResponse{Status: "ok", Version: local.Version, ProtocolVersion: local.ProtocolVersion,
		Commit: "expected-sha", ToolCount: local.ToolCount, CatalogHash: local.CatalogHash}

	headerServer := versionServer(t, remote, func(header http.Header) {
		header.Set("X-MCP-Catalog-Hash", "sha256:"+strings.Repeat("f", 64))
	})
	defer headerServer.Close()
	if err := run([]string{"--url", headerServer.URL, "--expected-commit", "expected-sha"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("header mismatch error = %v", err)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(strings.Repeat("x", maxVersionResponseBytes+1)))
	}))
	defer largeServer.Close()
	if err := run([]string{"--url", largeServer.URL, "--expected-commit", "expected-sha"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestVersionEndpointValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
	}{
		{name: "missing", url: ""},
		{name: "insecure remote", url: "http://example.com"},
		{name: "credentials", url: "https://user:pass@example.com"},
		{name: "query", url: "https://example.com?token=x"},
		{name: "unexpected path", url: "https://example.com/admin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := versionEndpoint(test.url); err == nil {
				t.Fatalf("versionEndpoint(%q) unexpectedly succeeded", test.url)
			}
		})
	}
}

func versionServer(t *testing.T, body versionResponse, mutateHeader func(http.Header)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/version" {
			t.Errorf("path = %q, want /version", request.URL.Path)
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Pragma", "no-cache")
		response.Header().Set("X-MCP-Server-Commit", body.Commit)
		response.Header().Set("X-MCP-Catalog-Hash", body.CatalogHash)
		response.Header().Set("X-MCP-Tool-Count", strconv.Itoa(body.ToolCount))
		if mutateHeader != nil {
			mutateHeader(response.Header())
		}
		_ = json.NewEncoder(response).Encode(body)
	}))
}
