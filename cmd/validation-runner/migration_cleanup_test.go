package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationCleanupEndpointDisabledByDefault(t *testing.T) {
	cfg := config{token: "01234567890123456789012345678901"}
	mux := newRunnerMux(cfg, false)
	registerMigrationCleanup(mux, cfg, false)
	req := httptest.NewRequest(http.MethodPost, migrationCleanupPath, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("disabled cleanup returned %d, want 404", res.Code)
	}
}

func TestMigrationCleanupEndpointRunsOnlyFixedAction(t *testing.T) {
	called := false
	cfg := config{
		token: "01234567890123456789012345678901",
		cleanupMigration: func(context.Context) (migrationCleanupResult, error) {
			called = true
			return migrationCleanupResult{CodexKeysRemoved: 1, LegacyContainerRemoved: true}, nil
		},
	}
	mux := newRunnerMux(cfg, false)
	registerMigrationCleanup(mux, cfg, true)
	req := httptest.NewRequest(http.MethodPost, migrationCleanupPath, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("cleanup returned %d: %s", res.Code, res.Body.String())
	}
	if !called || !strings.Contains(res.Body.String(), `"codex_keys_removed":1`) {
		t.Fatalf("unexpected cleanup result: %s", res.Body.String())
	}
}

func TestMigrationCleanupEndpointRequiresPostAndBearer(t *testing.T) {
	cfg := config{
		token: "01234567890123456789012345678901",
		cleanupMigration: func(context.Context) (migrationCleanupResult, error) {
			return migrationCleanupResult{}, nil
		},
	}
	for _, tc := range []struct {
		method string
		token  string
	}{
		{method: http.MethodGet, token: cfg.token},
		{method: http.MethodPost, token: "wrong"},
	} {
		mux := newRunnerMux(cfg, false)
		registerMigrationCleanup(mux, cfg, true)
		req := httptest.NewRequest(tc.method, migrationCleanupPath, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s returned %d, want 401", tc.method, res.Code)
		}
	}
}

func TestMigrationHostCleanupArgvIsClosed(t *testing.T) {
	joined := strings.Join(migrationHostCleanupArgv(), " ")
	for _, required := range []string{
		"--network none",
		"--read-only",
		"--cap-drop ALL",
		"no-new-privileges",
		"src=/root,dst=/host-root",
		"src=/run,dst=/host-run",
		"codex-validation-runner-migration-2026",
		"claude-validation-runner-migration-2026",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("cleanup argv missing %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"--network bridge", "/var/lib/docker/volumes", "MCP_DEVBOX_VALIDATION_RUNNER_TOKEN"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("cleanup argv contains forbidden %q: %s", forbidden, joined)
		}
	}
}
