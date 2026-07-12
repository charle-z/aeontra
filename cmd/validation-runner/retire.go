package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	legacyRetirePath      = "/v1/admin/retire-legacy-runner"
	legacyRunnerContainer = "mcp-devbox-validation-runner"
)

func newRunnerMux(cfg config, allowLegacyRetire bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok validation-runner\n")
	})
	mux.HandleFunc(runPath, cfg.handleRun)
	if allowLegacyRetire {
		mux.HandleFunc(legacyRetirePath, cfg.handleLegacyRetire)
	}
	return mux
}

func (c config) handleLegacyRetire(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !constantBearer(r.Header.Get("Authorization"), c.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if c.retireLegacy == nil {
		http.Error(w, "retirement action unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := c.retireLegacy(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "legacy runner retirement:", err)
		http.Error(w, "legacy runner retirement failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"stopped": true})
}

var dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}

func stopLegacyRunner(ctx context.Context) error {
	output, err := dockerCommand(ctx, "inspect", "--format", "{{.State.Status}}", legacyRunnerContainer)
	if err != nil {
		return fmt.Errorf("docker inspect failed: %s: %w", boundedDiagnostic(output), err)
	}
	state := strings.TrimSpace(string(output))
	if state == "exited" || state == "dead" {
		return nil
	}
	if state != "running" && state != "restarting" {
		return fmt.Errorf("legacy runner has unexpected state %q", state)
	}
	output, err = dockerCommand(ctx, "stop", "--time=30", legacyRunnerContainer)
	if err != nil {
		return fmt.Errorf("docker stop failed: %s: %w", boundedDiagnostic(output), err)
	}
	if strings.TrimSpace(string(output)) != legacyRunnerContainer {
		return fmt.Errorf("unexpected docker stop response: %s", boundedDiagnostic(output))
	}
	return nil
}

func boundedDiagnostic(output []byte) string {
	text := strings.TrimSpace(string(output))
	if len(text) > 512 {
		text = text[:512] + "…"
	}
	if text == "" {
		return "no diagnostic output"
	}
	return text
}

func boolEnv(name string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true")
}
