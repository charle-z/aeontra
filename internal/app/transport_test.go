package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func clearTransportEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		tokenEnv, publicURLEnv, oauthPassphraseEnv, oauthClientStorePathEnv, oauthRefreshStorePathEnv, stateRootEnv,
	} {
		t.Setenv(name, "")
	}
}

func TestNormalizeHTTPAddrPreservesLoopbackDefault(t *testing.T) {
	for input, want := range map[string]string{
		"8765":           "127.0.0.1:8765",
		":8765":          "127.0.0.1:8765",
		"  :9000  ":      "127.0.0.1:9000",
		"0.0.0.0:8765":   "0.0.0.0:8765",
		"localhost:8765": "localhost:8765",
	} {
		if got := normalizeHTTPAddr(input); got != want {
			t.Errorf("normalizeHTTPAddr(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveTransportKeepsStdioAuthFree(t *testing.T) {
	clearTransportEnv(t)
	cfg, err := resolveTransport(serveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != transportStdio || cfg.Token != "" || cfg.OAuth != nil {
		t.Fatalf("transport = %#v", cfg)
	}
}

func TestResolveTransportFailsClosedWithoutHTTPAuth(t *testing.T) {
	clearTransportEnv(t)
	_, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
	if err == nil || !strings.Contains(err.Error(), "HTTP transport requires auth") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveTransportPreservesTokenPrecedence(t *testing.T) {
	clearTransportEnv(t)
	t.Setenv(tokenEnv, "env-token")

	fromEnv, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
	if err != nil {
		t.Fatal(err)
	}
	if fromEnv.Token != "env-token" || fromEnv.Addr != "127.0.0.1:8765" {
		t.Fatalf("env transport = %#v", fromEnv)
	}

	fromFlag, err := resolveTransport(serveOptions{HTTPAddr: "0.0.0.0:8765", HTTPToken: "flag-token"})
	if err != nil {
		t.Fatal(err)
	}
	if fromFlag.Token != "flag-token" || fromFlag.Addr != "0.0.0.0:8765" {
		t.Fatalf("flag transport = %#v", fromFlag)
	}
}

func TestResolveTransportPreservesOAuthContract(t *testing.T) {
	clearTransportEnv(t)
	t.Setenv(publicURLEnv, "https://mcp.example")
	if _, err := resolveTransport(serveOptions{HTTPAddr: ":8765"}); err == nil || !strings.Contains(err.Error(), "OAuth requires BOTH") {
		t.Fatalf("partial OAuth error = %v", err)
	}

	t.Setenv(oauthPassphraseEnv, strings.Repeat("p", 32))
	cfg, err := resolveTransport(serveOptions{HTTPAddr: ":8765"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != transportHTTP || cfg.OAuth == nil || cfg.Token != "" {
		t.Fatalf("OAuth transport = %#v", cfg)
	}
	if !strings.Contains(cfg.AuthDescription, "OAuth") {
		t.Fatalf("auth description = %q", cfg.AuthDescription)
	}
}

func TestResolveOAuthStorePathsDefaultsToConfiguredStateRoot(t *testing.T) {
	clearTransportEnv(t)
	root := filepath.Join(t.TempDir(), "state")
	client, refresh := resolveOAuthStorePaths(root)
	if want := filepath.Join(root, "oauth-clients.json"); client != want {
		t.Fatalf("client store = %q, want %q", client, want)
	}
	if want := filepath.Join(root, "oauth-refresh.json"); refresh != want {
		t.Fatalf("refresh store = %q, want %q", refresh, want)
	}
}

func TestResolveOAuthStorePathsPreservesExplicitOverrides(t *testing.T) {
	clearTransportEnv(t)
	root := filepath.Join(t.TempDir(), "state")
	explicitClient := filepath.Join(t.TempDir(), "clients.json")
	explicitRefresh := filepath.Join(t.TempDir(), "refresh.json")
	t.Setenv(oauthClientStorePathEnv, explicitClient)
	t.Setenv(oauthRefreshStorePathEnv, explicitRefresh)
	client, refresh := resolveOAuthStorePaths(root)
	if client != explicitClient || refresh != explicitRefresh {
		t.Fatalf("stores = (%q, %q), want (%q, %q)", client, refresh, explicitClient, explicitRefresh)
	}
}

func TestResolveOAuthStorePathsKeepsLocalMemoryOnlyWithoutStateRoot(t *testing.T) {
	clearTransportEnv(t)
	client, refresh := resolveOAuthStorePaths("")
	if client != "" || refresh != "" {
		t.Fatalf("memory-only stores = (%q, %q), want empty", client, refresh)
	}
}
