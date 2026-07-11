// Command validation-runner is the private companion for reproducible JavaScript
// validation. It is intentionally not an MCP server: it accepts only two fixed
// profiles and is meant to run on the private Docker host network.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	runPath      = "/v1/run"
	maxBodyBytes = 4 << 10
	maxOutput    = 1 << 20
)

type config struct {
	token    string
	root     string // runner-container path used to inspect the repository
	hostRoot string // Docker-host path used only in the child bind mount
	image    string
	store    string
	user     string
	timeout  time.Duration
}

type request struct {
	Repo    string `json:"repo"`
	Profile string `json:"profile"`
}

type response struct {
	Profile  string `json:"profile"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "validation-runner:", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok validation-runner\n") })
	mux.HandleFunc(runPath, cfg.handleRun)
	addr := valueOr("MCP_DEVBOX_VALIDATION_RUNNER_ADDR", ":8787")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintln(os.Stderr, "validation-runner listening on", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "validation-runner:", err)
		os.Exit(1)
	}
}

func loadConfig() (config, error) {
	token := strings.TrimSpace(os.Getenv("MCP_DEVBOX_VALIDATION_RUNNER_TOKEN"))
	if len(token) < 32 {
		return config{}, fmt.Errorf("MCP_DEVBOX_VALIDATION_RUNNER_TOKEN must contain at least 32 characters")
	}
	root := strings.TrimSpace(os.Getenv("MCP_DEVBOX_VALIDATION_RUNNER_ROOT"))
	if !filepath.IsAbs(root) {
		return config{}, fmt.Errorf("MCP_DEVBOX_VALIDATION_RUNNER_ROOT must be an absolute container path")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return config{}, fmt.Errorf("resolving runner root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return config{}, fmt.Errorf("runner root is not a directory")
	}
	hostRoot := strings.TrimSpace(os.Getenv("MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT"))
	if !filepath.IsAbs(hostRoot) {
		return config{}, fmt.Errorf("MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT must be an absolute Docker-host path")
	}
	return config{
		token: token, root: resolved, hostRoot: filepath.Clean(hostRoot),
		image:   valueOr("MCP_DEVBOX_VALIDATION_RUNNER_IMAGE", "node:22-alpine"),
		store:   valueOr("MCP_DEVBOX_VALIDATION_RUNNER_STORE", "mcp-devbox-pnpm-store"),
		user:    valueOr("MCP_DEVBOX_VALIDATION_RUNNER_USER", "10001:10001"),
		timeout: durationOr("MCP_DEVBOX_VALIDATION_RUNNER_TIMEOUT", 8*time.Minute),
	}, nil
}

func (c config) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !constantBearer(r.Header.Get("Authorization"), c.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	repo, err := c.repoPath(req.Repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	argv, err := c.argv(repo, req.Profile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), c.timeout)
	defer cancel()
	out, code, err := dockerRun(ctx, argv)
	if err != nil {
		http.Error(w, "runner failure: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{Profile: req.Profile, ExitCode: code, Output: out})
}

func (c config) repoPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) || strings.ContainsAny(name, `\\/`) || name == "." || name == ".." {
		return "", fmt.Errorf("repo must be one direct repository name under the configured root")
	}
	p, err := filepath.EvalSymlinks(filepath.Join(c.root, name))
	if err != nil || filepath.Dir(p) != c.root {
		return "", fmt.Errorf("repository is outside the configured root")
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("repository does not exist")
	}
	if _, err := os.Stat(filepath.Join(p, "package.json")); err != nil {
		return "", fmt.Errorf("repository has no package.json")
	}
	return p, nil
}

func (c config) argv(repo, profile string) ([]string, error) {
	// Scripts are constants owned by this binary. The project can influence only
	// package metadata, never Docker flags, executable names, argv or environment.
	script := ""
	network := "none"
	switch profile {
	case "pnpm-lockfile":
		network = "bridge" // required solely to resolve/fetch the declared dependency graph.
		// Do not use `corepack enable`: it writes shims into the image filesystem,
		// which is intentionally read-only. `corepack pnpm` uses only COREPACK_HOME
		// under /tmp and keeps the exact package-manager version fixed.
		script = "corepack prepare pnpm@10.13.1 --activate && corepack pnpm install --lockfile-only --ignore-scripts --registry=https://registry.npmjs.org && corepack pnpm fetch --ignore-scripts --registry=https://registry.npmjs.org"
	case "pnpm-validate":
		script = "corepack prepare pnpm@10.13.1 --activate && corepack pnpm install --offline --frozen-lockfile --ignore-scripts && corepack pnpm run check && corepack pnpm test && corepack pnpm run build"
	default:
		return nil, fmt.Errorf("unsupported validation profile")
	}
	// `repo` is resolved inside this runner container, while Docker resolves bind
	// sources on its host. Convert exactly one already-validated child path to the
	// configured host-side repository directory; never pass an agent-supplied path.
	rel, err := filepath.Rel(c.root, repo)
	if err != nil || rel == "." || strings.ContainsAny(rel, `\\/`) {
		return nil, fmt.Errorf("validated repository is not a direct child of runner root")
	}
	hostRepo := filepath.Join(c.hostRoot, rel)
	return []string{
		"run", "--rm", "--network", network, "--read-only",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", "256", "--memory", "1024m", "--cpus", "2",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=256m",
		"--user", c.user,
		"--mount", "type=bind,src=" + hostRepo + ",dst=/workspace",
		"--mount", "type=volume,src=" + c.store + ",dst=/pnpm-store",
		"--workdir", "/workspace",
		"-e", "COREPACK_HOME=/pnpm-store/corepack", "-e", "PNPM_HOME=/tmp/pnpm", "-e", "PNPM_STORE_DIR=/pnpm-store",
		c.image, "sh", "-ec", script,
	}, nil
}

func dockerRun(ctx context.Context, argv []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "docker", argv...)
	var buf limitedBuffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return buf.String(), code, ctx.Err()
	}
	if _, ok := err.(*exec.ExitError); ok {
		err = nil
	}
	if code != 0 && strings.TrimSpace(buf.String()) == "" {
		return "container exited without diagnostic output; inspect the private runner logs and verify repository mount permissions", code, err
	}
	return buf.String(), code, err
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len() >= maxOutput {
		return len(p), nil
	}
	remaining := maxOutput - b.Len()
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		_, _ = b.Buffer.WriteString("\n[output truncated]\n")
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func constantBearer(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	return len(got) == len(token) && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
func valueOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func durationOr(name string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return fallback
}
