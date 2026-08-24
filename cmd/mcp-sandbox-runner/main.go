package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxexecutor"
	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const (
	addrEnv       = "MCP_DEVBOX_SANDBOX_RUNNER_ADDR"
	tokenEnv      = "MCP_DEVBOX_SANDBOX_RUNNER_TOKEN"
	workspaceEnv  = "MCP_DEVBOX_SANDBOX_WORKSPACE_ID"
	rootEnv       = "MCP_DEVBOX_SANDBOX_RUNNER_WORKSPACE_ROOT"
	stateEnv      = "MCP_DEVBOX_SANDBOX_RUNNER_STATE_ROOT"
	imageEnv      = "MCP_DEVBOX_SANDBOX_IMAGE"
	socketEnv     = "MCP_DEVBOX_SANDBOX_RUNNER_PODMAN_SOCKET"
	maxTimeoutEnv = "MCP_DEVBOX_SANDBOX_MAX_TIMEOUT_MS"
	maxCPUEnv     = "MCP_DEVBOX_SANDBOX_MAX_CPU_MILLIS"
	maxMemoryEnv  = "MCP_DEVBOX_SANDBOX_MAX_MEMORY_MIB"
	maxPIDsEnv    = "MCP_DEVBOX_SANDBOX_MAX_PROCESSES"
	maxOutputEnv  = "MCP_DEVBOX_SANDBOX_MAX_OUTPUT_BYTES"
	maxRunsEnv    = "MCP_DEVBOX_SANDBOX_MAX_CONCURRENT"
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "healthcheck" {
			log.Fatal("unsupported command")
		}
		if err := runHealthcheck(); err != nil {
			log.Fatal(err)
		}
		return
	}
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

func runHealthcheck() error {
	baseURL, err := runnerHealthcheckURL(os.Getenv(addrEnv))
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("sandbox runner healthcheck redirect rejected")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return checkRunnerHealth(ctx, baseURL, strings.TrimSpace(os.Getenv(tokenEnv)), client)
}

func runnerHealthcheckURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = "127.0.0.1:8770"
	}
	if err := validateListenAddress(address); err != nil {
		return "", errors.New("sandbox runner healthcheck address is invalid")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "", errors.New("sandbox runner healthcheck address is invalid")
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func checkRunnerHealth(ctx context.Context, baseURL, token string, client *http.Client) error {
	if client == nil || strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" {
		return errors.New("sandbox runner healthcheck authority is incomplete")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/status", nil)
	if err != nil {
		return errors.New("sandbox runner healthcheck request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return errors.New("sandbox runner healthcheck request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("sandbox runner healthcheck was rejected")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4097))
	decoder.DisallowUnknownFields()
	var status sandboxprotocol.Status
	if err := decoder.Decode(&status); err != nil {
		return errors.New("sandbox runner healthcheck response is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("sandbox runner healthcheck response has trailing data")
	}
	if !status.Available || !status.Rootless {
		return errors.New("sandbox runner is not available on a rootless engine")
	}
	return nil
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
	engine, err := sandboxexecutor.NewPodmanEngine(strings.TrimSpace(os.Getenv(socketEnv)))
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
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("%s must be an IP:port address", addrEnv)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%s must use a literal loopback or private IP", addrEnv)
	}
	if ip.IsUnspecified() || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return fmt.Errorf("%s must bind only a loopback or private IP", addrEnv)
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
