package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/observability"
)

func clearRuntimeEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		tokenEnv, publicURLEnv, oauthPassphraseEnv, brainRootEnv, stateRootEnv,
		oauthClientStorePathEnv, oauthAccessStorePathEnv, oauthRefreshStorePathEnv,
		observabilityModeEnv, observabilityPathEnv, observabilityMaxBytesEnv,
		sandboxImageEnv,
		sandboxRunnerURLEnv, sandboxRunnerTokenEnv, sandboxWorkspaceIDEnv,
		validationRunnerURLEnv, validationRunnerTokenEnv,
		privilegedTasksEnv, privilegedServicesEnv, privilegedTimeoutEnv,
		maintainerProfileEnv,
		githubTokenEnv, githubOSSTokenEnv, githubOwnerEnv, githubOwnerTypeEnv, githubDefaultVisibilityEnv,
		coolifyURLEnv, coolifyAPITokenEnv, coolifyAllowedAppsEnv, coolifyServerUUIDEnv,
		coolifyProjectUUIDEnv, coolifyEnvironmentNameEnv, coolifyEnvironmentUUIDEnv,
		coolifyAllowedDomainsEnv, coolifyGitHubAppUUIDEnv, coolifyDestinationUUIDEnv,
		coolifyAllowedMountsEnv,
	} {
		t.Setenv(name, "")
	}
}

func TestLoadMaintainerProfileIsExplicitAndClosed(t *testing.T) {
	clearRuntimeEnv(t)
	profile, err := loadMaintainerProfile()
	if err != nil || profile != "" {
		t.Fatalf("default profile=%q err=%v", profile, err)
	}

	t.Setenv(maintainerProfileEnv, "charle-z-production")
	profile, err = loadMaintainerProfile()
	if err != nil || profile != "charle-z-production" {
		t.Fatalf("configured profile=%q err=%v", profile, err)
	}

	t.Setenv(maintainerProfileEnv, "unexpected")
	if _, err := loadMaintainerProfile(); err == nil || !strings.Contains(err.Error(), maintainerProfileEnv) {
		t.Fatalf("unsupported profile error=%v", err)
	}
}

func TestLoadPrivilegedConfigPreservesEnvironmentContract(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv(privilegedTasksEnv, "true")
	t.Setenv(privilegedServicesEnv, "alpha,beta_service")
	t.Setenv(privilegedTimeoutEnv, "45s")

	cfg, err := loadPrivilegedConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Timeout != 45*time.Second {
		t.Fatalf("privileged config = %#v", cfg)
	}
	if strings.Join(cfg.AllowedServices, ",") != "alpha,beta_service" {
		t.Fatalf("services = %#v", cfg.AllowedServices)
	}
}

func TestLoadPrivilegedConfigRejectsInvalidTimeout(t *testing.T) {
	clearRuntimeEnv(t)
	t.Setenv(privilegedTimeoutEnv, "0s")
	if _, err := loadPrivilegedConfig(); err == nil || !strings.Contains(err.Error(), privilegedTimeoutEnv) {
		t.Fatalf("error = %v", err)
	}
}

func TestOptionalRuntimeClientsUseExistingEnvironmentNames(t *testing.T) {
	clearRuntimeEnv(t)
	if githubOSSTokenEnv != "GH_TOKEN" {
		t.Fatalf("public OSS GitHub env = %q, want GH_TOKEN", githubOSSTokenEnv)
	}
	if buildGitHubClientFromEnv() != nil {
		t.Fatal("GitHub client should be disabled without GITHUB_TOKEN")
	}
	if buildCoolifyClientFromEnv() != nil {
		t.Fatal("Coolify client should be disabled without COOLIFY_URL")
	}
	if buildValidationRunnerFromEnv().Available() {
		t.Fatal("validation runner should be disabled without its URL/token")
	}

	t.Setenv(githubTokenEnv, "test-token")
	t.Setenv(githubOwnerEnv, "charle-z")
	t.Setenv(githubOwnerTypeEnv, "user")
	t.Setenv(githubDefaultVisibilityEnv, "private")
	if buildGitHubClientFromEnv() == nil {
		t.Fatal("GitHub client was not configured from existing env names")
	}

	t.Setenv(coolifyURLEnv, "https://coolify.example")
	t.Setenv(coolifyAPITokenEnv, "test-token")
	t.Setenv(coolifyAllowedAppsEnv, "app-a,app-b")
	t.Setenv(coolifyAllowedDomainsEnv, "example.com")
	if buildCoolifyClientFromEnv() == nil {
		t.Fatal("Coolify client was not configured from existing env names")
	}

	t.Setenv(validationRunnerURLEnv, "http://127.0.0.1:9090")
	t.Setenv(validationRunnerTokenEnv, strings.Repeat("x", 32))
	if !buildValidationRunnerFromEnv().Available() {
		t.Fatal("validation runner was not configured from existing env names")
	}
}

func TestBuildSandboxRunnerPreservesBackendPosture(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()

	pending := buildSandboxRunner(config.Config{SandboxBackend: "gvisor"}, root).Status(context.Background())
	if pending.Available || pending.Backend != "gvisor" || pending.FreeTerminal {
		t.Fatalf("pending sandbox status = %#v", pending)
	}

	t.Setenv(sandboxImageEnv, "golang:1.26-alpine")
	docker := buildSandboxRunner(config.Config{SandboxBackend: "docker"}, root).Status(context.Background())
	if docker.Available || docker.Backend != "docker" || docker.FreeTerminal {
		t.Fatalf("docker sandbox status = %#v", docker)
	}

	private := buildSandboxRunner(config.Config{SandboxBackend: "private-rootless"}, root).Status(context.Background())
	if private.Available || private.FreeTerminal || private.Backend != "private-rootless" {
		t.Fatalf("incompletely configured private sandbox status = %#v", private)
	}
}

func TestBuildRuntimeComposesPolicyAuditServiceAndServer(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	auditPath := filepath.Join(root, "logs", "audit.jsonl")
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
		TestCommand:     []string{"go", "test", "./..."},
		SandboxBackend:  "none",
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := buildRuntime(serveOptions{Config: cfg, AuditPath: auditPath})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	if runtime.PrimaryRoot != filepath.Clean(root) || runtime.AuditPath != auditPath {
		t.Fatalf("runtime paths = %#v", runtime)
	}
	if runtime.Policy == nil || runtime.Service == nil || runtime.Server == nil || runtime.Logger == nil || runtime.Observer == nil {
		t.Fatal("runtime composition is incomplete")
	}
	if runtime.Policy.Mode() != config.ModeReadOnly {
		t.Fatalf("mode = %q", runtime.Policy.Mode())
	}
	if status := runtime.Service.SandboxStatus(); !strings.Contains(status, "backend: none") {
		t.Fatalf("sandbox status = %q", status)
	}
}

func TestBuildRuntimeUsesBoundedPersistentStateLayout(t *testing.T) {
	clearRuntimeEnv(t)
	repo := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	cfg, err := config.New(config.Config{Roots: []string{repo}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}, SandboxBackend: "none"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildRuntime(serveOptions{Config: cfg, StateRoot: state, Observability: observability.Config{Mode: observability.ModeFile, MaxBytes: observability.MinMaxBytes}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"telemetry/metrics.db", "logs/observability.jsonl", "logs/audit.jsonl", "results/results.db", "model-turns/model-turns.db"} {
		if _, err := os.Stat(filepath.Join(state, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
	}
}

func TestBuildRuntimePersistsMetricsWhenJSONLIsOff(t *testing.T) {
	clearRuntimeEnv(t)
	repo := t.TempDir()
	state := filepath.Join(t.TempDir(), "state")
	cfg, err := config.New(config.Config{Roots: []string{repo}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}, SandboxBackend: "none"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildRuntime(serveOptions{Config: cfg, StateRoot: state, Observability: observability.Config{Mode: observability.ModeOff, MaxBytes: observability.DefaultMaxBytes}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Observer.Emit(observability.Event{Name: observability.EventHTTPRequest, Transport: observability.TransportHTTP, Route: observability.RouteHealth, Outcome: observability.OutcomeSuccess, StatusCode: 200, HTTPDurationMS: 2}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.Telemetry.Snapshot("hourly")
	if err != nil || snapshot.RequestCount != 1 || snapshot.HTTPDurationMS != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRuntimeOpensPrivateObservabilityFile(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	path := filepath.Join(root, "private", "observability.jsonl")
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git"},
		SandboxBackend:  "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildRuntime(serveOptions{
		Config: cfg,
		Observability: observability.Config{
			Mode:     observability.ModeFile,
			Path:     path,
			MaxBytes: observability.MinMaxBytes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Observer.Emit(observability.Event{Level: observability.LevelInfo, Component: observability.ComponentServer, Name: observability.EventServerStart}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"event":"server_start"`) {
		t.Fatalf("events = %s", data)
	}
}
