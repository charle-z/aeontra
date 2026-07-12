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
		http.Error(w, "legacy runner retirement failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"stopped": true})
}

func stopLegacyRunner(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", "--time", "30", legacyRunnerContainer)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop failed: %w", err)
	}
	if strings.TrimSpace(string(output)) != legacyRunnerContainer {
		return fmt.Errorf("unexpected docker stop response")
	}
	return nil
}

func boolEnv(name string) bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true")
}
