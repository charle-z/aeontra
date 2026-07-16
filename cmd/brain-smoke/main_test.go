package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func brainSmokeServer(t *testing.T, configured bool) (*httptest.Server, mcpserver.RuntimeInfo, string, []string) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{repoRoot}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git", "go"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), repoRoot)
	privateValues := []string{}
	if configured {
		brainRoot := filepath.Join(t.TempDir(), "brain")
		store, err := brainpkg.OpenStore(brainRoot, time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.InitializeGit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := store.OpenIndex(context.Background()); err != nil {
			t.Fatal(err)
		}
		service.WithBrainStore(store)
		draft := brainpkg.AgentDraft{
			Slug:       "private-smoke-note",
			Title:      "Private smoke title",
			Type:       brainpkg.TypeNote,
			Author:     "agent:smoke",
			Provenance: "private smoke provenance",
			ReviewBy:   "2026-08-13",
			Body:       "PRIVATE SMOKE BODY MUST NEVER BE PRINTED.",
		}
		if _, err := service.BrainWrite(context.Background(), draft); err != nil {
			t.Fatal(err)
		}
		privateValues = append(privateValues, brainRoot, draft.Slug, draft.Title, draft.Provenance, draft.Body)
	}
	t.Cleanup(func() { _ = service.BrainCapability.Close() })
	mcp := mcpserver.New(service)
	info, err := mcp.RuntimeInfo()
	if err != nil {
		t.Fatal(err)
	}
	const token = "brain-smoke-private-token"
	server := httptest.NewServer(mcp.HTTPHandler(token, nil))
	return server, info, token, privateValues
}

func TestRunValidatesConfiguredBrainWithoutPrintingPrivateData(t *testing.T) {
	server, info, token, privateValues := brainSmokeServer(t, true)
	defer server.Close()
	var output bytes.Buffer
	err := run([]string{"--url", server.URL, "--expected-commit", info.Commit}, &output, func(name string) string {
		if name == defaultBearerEnv {
			return token
		}
		return ""
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"brain smoke passed",
		"commit=" + info.Commit,
		"tool_count=78",
		info.CatalogHash,
		"index_ready=true",
		"schema_version=1",
		"note_count=1",
		"context_bytes=",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("output missing %q: %s", required, output.String())
		}
	}
	for _, forbidden := range append(privateValues, token) {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("output leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestRunRejectsDisabledBrainWithoutLeakingCredential(t *testing.T) {
	server, info, token, _ := brainSmokeServer(t, false)
	defer server.Close()
	err := run([]string{"--url", server.URL, "--expected-commit", info.Commit}, &bytes.Buffer{}, func(string) string { return token }, server.Client())
	if err == nil || !strings.Contains(err.Error(), "brain tool") || strings.Contains(err.Error(), token) {
		t.Fatalf("error=%v", err)
	}
}

func TestRunValidatesArgumentsAndBearerEnvironment(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--url", "http://example.com", "--expected-commit", "sha"},
		{"--url", "https://user:pass@example.com", "--expected-commit", "sha"},
		{"--url", "https://example.com/mcp?key=x", "--expected-commit", "sha"},
		{"--url", "https://example.com/private", "--expected-commit", "sha"},
		{"--url", "https://example.com", "--expected-commit", "sha", "--bearer-env", "bad-name"},
		{"--url", "https://example.com", "--expected-commit", "sha", "--timeout", "0s"},
	} {
		if err := run(args, &bytes.Buffer{}, func(string) string { return "token" }, nil); err == nil {
			t.Fatalf("args=%v unexpectedly succeeded", args)
		}
	}
	if err := run([]string{"--url", "https://example.com", "--expected-commit", "sha"}, &bytes.Buffer{}, func(string) string { return "" }, nil); err == nil || strings.Contains(err.Error(), "token") {
		t.Fatalf("empty credential error=%v", err)
	}
}

func TestMCPEndpointAllowsOnlyHTTPSOrLoopback(t *testing.T) {
	for _, raw := range []string{"https://example.com", "https://example.com/mcp", "http://127.0.0.1:8765/mcp", "http://localhost:8765"} {
		endpoint, err := mcpEndpoint(raw)
		if err != nil || endpoint.Path != "/mcp" {
			t.Fatalf("mcpEndpoint(%q)=%v err=%v", raw, endpoint, err)
		}
	}
}
