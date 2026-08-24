package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxexecutor"
)

const (
	addrEnv       = "MCP_DEVBOX_SANDBOX_RUNNER_ADDR"
	tokenEnv      = "MCP_DEVBOX_SANDBOX_RUNNER_TOKEN"
	workspaceEnv  = "MCP_DEVBOX_SANDBOX_WORKSPACE_ID"
	rootEnv       = "MCP_DEVBOX_SANDBOX_RUNNER_WORKSPACE_ROOT"
	stateEnv      = "MCP_DEVBOX_SANDBOX_RUNNER_STATE_ROOT"
	imageEnv      = "MCP_DEVBOX_SANDBOX_IMAGE"
	podmanEnv     = "MCP_DEVBOX_SANDBOX_RUNNER_PODMAN"
	socketEnv     = "MCP_DEVBOX_SANDBOX_RUNNER_PODMAN_SOCKET"
	maxTimeoutEnv = "MCP_DEVBOX_SANDBOX_MAX_TIMEOUT_MS"
	maxCPUEnv     = "MCP_DEVBOX_SANDBOX_MAX_CPU_MILLIS"
	maxMemoryEnv  = "MCP_DEVBOX_SANDBOX_MAX_MEMORY_MIB"
	maxPIDsEnv    = "MCP_DEVBOX_SANDBOX_MAX_PROCESSES"
	maxOutputEnv  = "MCP_DEVBOX_SANDBOX_MAX_OUTPUT_BYTES"
	maxRunsEnv    = "MCP_DEVBOX_SANDBOX_MAX_CONCURRENT"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{
		Addr: config.address, Handler: config.executor.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 31 * time.Minute, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	log.Printf("private sandbox runner listening on %s", config.address)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type runnerConfig struct {
	address  string
	executor *sandboxexecutor.Executor
}

func loadConfig() (runnerConfig, error) {
	address := strings.TrimSpace(os.Getenv(addrEnv))
	if address == "" {
		address = "127.0.0.1:8770"
	}
	if err := validateListenAddress(address); err != nil {
		return runnerConfig{}, err
	}
	image := strings.ToLower(strings.TrimSpace(os.Getenv(imageEnv)))
	separator := strings.LastIndex(image, "@")
	if separator < 0 {
		return runnerConfig{}, fmt.Errorf("%s must pin an image by sha256 digest", imageEnv)
	}
	digest := image[separator+1:]
	podman := strings.TrimSpace(os.Getenv(podmanEnv))
	if podman == "" {
		podman = "/usr/bin/podman"
	}
	engine, err := sandboxexecutor.NewPodmanEngine(podman, strings.TrimSpace(os.Getenv(socketEnv)))
	if err != nil {
		return runnerConfig{}, err
	}
	executor, err := sandboxexecutor.New(sandboxexecutor.Config{
		Token: strings.TrimSpace(os.Getenv(tokenEnv)), WorkspaceID: strings.TrimSpace(os.Getenv(workspaceEnv)),
		WorkspaceRoot: strings.TrimSpace(os.Getenv(rootEnv)), StateRoot: strings.TrimSpace(os.Getenv(stateEnv)),
		Image: image, ImageDigest: digest,
		MaxTimeoutMS: int64Env(maxTimeoutEnv, 120000), MaxCPUMillis: intEnv(maxCPUEnv, 1000),
		MaxMemoryMiB: intEnv(maxMemoryEnv, 1024), MaxProcessLimit: intEnv(maxPIDsEnv, 256),
		MaxOutputBytes: intEnv(maxOutputEnv, 1<<20), MaxConcurrent: intEnv(maxRunsEnv, 2), Engine: engine,
	})
	if err != nil {
		return runnerConfig{}, err
	}
	return runnerConfig{address: address, executor: executor}, nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("%s must be a host:port address", addrEnv)
	}
	if parsed := net.ParseIP(host); parsed != nil && !parsed.IsLoopback() && !parsed.IsPrivate() && !parsed.IsUnspecified() {
		return fmt.Errorf("%s must bind only loopback, private, or an unexposed container interface", addrEnv)
	}
	return nil
}

func intEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return -1
	}
	return value
}

func int64Env(name string, fallback int64) int64 {
	return int64(intEnv(name, int(fallback)))
}
