package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type appRuntime struct {
	Policy      *policy.Policy
	Logger      *audit.Logger
	Observer    *observability.Logger
	Service     *tools.Service
	Server      *mcpserver.Server
	PrimaryRoot string
	AuditPath   string
}

func (r *appRuntime) Close() error {
	if r == nil {
		return nil
	}
	var serviceErr, auditErr, observabilityErr error
	if r.Service != nil {
		serviceErr = r.Service.BrainCapability.Close()
	}
	if r.Logger != nil {
		auditErr = r.Logger.Close()
	}
	if r.Observer != nil {
		observabilityErr = r.Observer.Close()
	}
	if serviceErr != nil || auditErr != nil || observabilityErr != nil {
		return errors.New("runtime close failed")
	}
	return nil
}

func buildRuntime(opts serveOptions) (*appRuntime, error) {
	pol, err := policy.NewPolicy(opts.Config)
	if err != nil {
		return nil, err
	}
	primary := pol.Roots()[0]
	auditPath := opts.AuditPath
	if auditPath == "" {
		auditPath = filepath.Join(primary, ".agent-memory", "audit.log")
	}
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating audit dir: %w", err)
	}
	logger, err := audit.Open(auditPath)
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	observabilityConfig, err := resolveObservabilityConfig(opts.Observability, primary)
	if err != nil {
		_ = logger.Close()
		return nil, err
	}
	observer, err := observability.Open(observabilityConfig, os.Stderr)
	if err != nil {
		_ = logger.Close()
		return nil, fmt.Errorf("opening observability sink: %w", err)
	}
	service, err := buildToolService(opts.Config, pol, logger, primary)
	if err != nil {
		_ = observer.Close()
		_ = logger.Close()
		return nil, err
	}
	return &appRuntime{
		Policy:      pol,
		Logger:      logger,
		Observer:    observer,
		Service:     service,
		Server:      mcpserver.NewWithObserver(service, observer),
		PrimaryRoot: primary,
		AuditPath:   auditPath,
	}, nil
}

func buildToolService(cfg config.Config, pol *policy.Policy, logger *audit.Logger, primary string) (*tools.Service, error) {
	service := tools.NewService(pol, logger, primary).
		WithTestCommand(cfg.TestCommand).
		WithSandboxRunner(buildSandboxRunner(cfg, primary)).
		WithValidationRunner(buildValidationRunnerFromEnv())

	privileged, err := loadPrivilegedConfig()
	if err != nil {
		return nil, err
	}
	service = service.WithPrivilegedConfig(privileged)
	if coolify := buildCoolifyClientFromEnv(); coolify != nil {
		service = service.WithCoolify(coolify)
	}
	if github := buildGitHubClientFromEnv(); github != nil {
		service = service.WithGitHub(github)
	}
	return service, nil
}

func buildSandboxRunner(cfg config.Config, primary string) tools.SandboxRunner {
	runner := tools.NewSandboxRunner(cfg.SandboxBackend)
	if cfg.SandboxBackend != "docker" {
		return runner
	}
	image := strings.TrimSpace(os.Getenv(sandboxImageEnv))
	if image == "" {
		image = "golang:1.26-alpine"
	}
	return tools.NewDockerSandboxRunner(tools.DockerSandboxConfig{Image: image, Root: primary})
}

func buildValidationRunnerFromEnv() tools.ValidationRunner {
	return tools.NewValidationRunner(os.Getenv(validationRunnerURLEnv), os.Getenv(validationRunnerTokenEnv))
}

func loadPrivilegedConfig() (tools.PrivilegedConfig, error) {
	timeout := 2 * time.Minute
	if raw := strings.TrimSpace(os.Getenv(privilegedTimeoutEnv)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return tools.PrivilegedConfig{}, fmt.Errorf("%s must be a positive duration", privilegedTimeoutEnv)
		}
		timeout = parsed
	}
	return tools.PrivilegedConfig{
		Enabled:         strings.EqualFold(strings.TrimSpace(os.Getenv(privilegedTasksEnv)), "true"),
		AllowedServices: splitCSV(os.Getenv(privilegedServicesEnv)),
		Timeout:         timeout,
	}, nil
}

func buildCoolifyClientFromEnv() *tools.CoolifyClient {
	baseURL := strings.TrimSpace(os.Getenv(coolifyURLEnv))
	if baseURL == "" {
		return nil
	}
	return tools.NewCoolifyClient(baseURL, os.Getenv(coolifyAPITokenEnv), splitCSV(os.Getenv(coolifyAllowedAppsEnv))).
		WithBuilderConfig(
			os.Getenv(coolifyServerUUIDEnv),
			os.Getenv(coolifyProjectUUIDEnv),
			os.Getenv(coolifyEnvironmentNameEnv),
			os.Getenv(coolifyEnvironmentUUIDEnv),
			splitCSV(os.Getenv(coolifyAllowedDomainsEnv)),
		).
		WithGitHubApp(os.Getenv(coolifyGitHubAppUUIDEnv)).
		WithBuilderRuntime(os.Getenv(coolifyDestinationUUIDEnv), splitSemicolon(os.Getenv(coolifyAllowedMountsEnv)))
}

func buildGitHubClientFromEnv() *tools.GitHubClient {
	token := strings.TrimSpace(os.Getenv(githubTokenEnv))
	if token == "" {
		return nil
	}
	return tools.NewGitHubClient("", token, os.Getenv(githubOwnerEnv), os.Getenv(githubOwnerTypeEnv), os.Getenv(githubDefaultVisibilityEnv))
}
