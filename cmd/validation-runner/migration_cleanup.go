package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	migrationCleanupPath = "/v1/admin/finalize-migration-cleanup"
	legacyRunnerImage    = "mcp-devbox-validation-runner:a1136f6"
)

type migrationCleanupResult struct {
	CodexKeysRemoved       int  `json:"codex_keys_removed"`
	ClaudeKeysRemoved      int  `json:"claude_keys_removed"`
	TempTokenFilesRemoved  int  `json:"temp_token_files_removed"`
	LegacyContainerRemoved bool `json:"legacy_container_removed"`
	LegacyImageRemoved     bool `json:"legacy_image_removed"`
}

func registerMigrationCleanup(mux *http.ServeMux, cfg config, enabled bool) {
	if enabled {
		mux.HandleFunc(migrationCleanupPath, cfg.handleMigrationCleanup)
	}
}

func (c config) handleMigrationCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !constantBearer(r.Header.Get("Authorization"), c.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if c.cleanupMigration == nil {
		http.Error(w, "migration cleanup unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := c.cleanupMigration(ctx)
	if err != nil {
		http.Error(w, "migration cleanup failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func cleanupMigrationArtifacts(ctx context.Context) (migrationCleanupResult, error) {
	result := migrationCleanupResult{}
	output, err := dockerCommand(ctx, migrationHostCleanupArgv()...)
	if err != nil {
		return result, fmt.Errorf("host cleanup helper failed: %s: %w", boundedDiagnostic(output), err)
	}
	values := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value, parseErr := strconv.Atoi(strings.TrimSpace(raw))
		if parseErr == nil {
			values[strings.TrimSpace(key)] = value
		}
	}
	result.CodexKeysRemoved = values["codex"]
	result.ClaudeKeysRemoved = values["claude"]
	result.TempTokenFilesRemoved = values["temp_tokens"]

	stateOutput, inspectErr := dockerCommand(ctx, "inspect", "--format", "{{.State.Status}}", legacyRunnerContainer)
	if inspectErr == nil {
		state := strings.TrimSpace(string(stateOutput))
		if state == "running" || state == "restarting" {
			return result, fmt.Errorf("legacy runner unexpectedly active; refusing deletion")
		}
		if state != "exited" && state != "dead" && state != "created" {
			return result, fmt.Errorf("legacy runner has unexpected state %q", state)
		}
		removeOutput, removeErr := dockerCommand(ctx, "rm", legacyRunnerContainer)
		if removeErr != nil || strings.TrimSpace(string(removeOutput)) != legacyRunnerContainer {
			return result, fmt.Errorf("legacy container removal failed: %s: %w", boundedDiagnostic(removeOutput), removeErr)
		}
		result.LegacyContainerRemoved = true
	} else if !strings.Contains(strings.ToLower(string(stateOutput)), "no such") {
		return result, fmt.Errorf("legacy container inspection failed: %s: %w", boundedDiagnostic(stateOutput), inspectErr)
	}

	imageOutput, imageErr := dockerCommand(ctx, "image", "inspect", legacyRunnerImage)
	if imageErr == nil {
		removeOutput, removeErr := dockerCommand(ctx, "image", "rm", legacyRunnerImage)
		if removeErr != nil {
			return result, fmt.Errorf("legacy image removal failed: %s: %w", boundedDiagnostic(removeOutput), removeErr)
		}
		result.LegacyImageRemoved = true
	} else if !strings.Contains(strings.ToLower(string(imageOutput)), "no such") {
		return result, fmt.Errorf("legacy image inspection failed: %s: %w", boundedDiagnostic(imageOutput), imageErr)
	}
	return result, nil
}

func migrationHostCleanupArgv() []string {
	const script = `set -eu
file=/host-root/.ssh/authorized_keys
codex=0
claude=0
temp_tokens=0
if [ -f "$file" ]; then
  codex=$(grep -c 'codex-validation-runner-migration-2026' "$file" || true)
  claude=$(grep -c 'claude-validation-runner-migration-2026' "$file" || true)
  awk 'index($0,"codex-validation-runner-migration-2026")==0 && index($0,"claude-validation-runner-migration-2026")==0' "$file" > /tmp/authorized_keys
  cat /tmp/authorized_keys > "$file"
  chmod 600 "$file"
fi
for path in /host-root/.coolify_api_token /host-run/coolify-migration/token; do
  if [ -f "$path" ]; then
    rm -f "$path"
    temp_tokens=$((temp_tokens+1))
  fi
done
rmdir /host-run/coolify-migration 2>/dev/null || true
printf 'codex=%s\nclaude=%s\ntemp_tokens=%s\n' "$codex" "$claude" "$temp_tokens"`
	return []string{
		"run", "--rm", "--network", "none", "--read-only",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
		"--mount", "type=bind,src=/root,dst=/host-root",
		"--mount", "type=bind,src=/run,dst=/host-run",
		"docker:29-cli", "sh", "-ec", script,
	}
}
