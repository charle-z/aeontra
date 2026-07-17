package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
	"github.com/charle-z/mcp-devbox/internal/taskjournal"
	"github.com/charle-z/mcp-devbox/internal/telemetry"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type appRuntime struct {
	Policy      *policy.Policy
	Logger      *audit.Logger
	Observer    *observability.Logger
	Service     *tools.Service
	Server      *mcpserver.Server
	Journal     *taskjournal.Journal
	PrimaryRoot string
	AuditPath   string
	StateRoot   string
	Telemetry   *telemetry.Store
	Results     *resultstore.Store
	ModelTurns  *modelturn.Store
	Edge        *edge.Store
}

func (r *appRuntime) Close() error {
	if r == nil {
		return nil
	}
	var serviceErr, auditErr, observabilityErr, telemetryErr, journalErr, resultErr, modelTurnErr, edgeErr error
	if r.Service != nil {
		serviceErr = r.Service.BrainCapability.Close()
	}
	if r.Logger != nil {
		auditErr = r.Logger.Close()
	}
	if r.Observer != nil {
		observabilityErr = r.Observer.Close()
	}
	if r.Telemetry != nil {
		telemetryErr = r.Telemetry.Close()
	}
	if r.Journal != nil {
		journalErr = r.Journal.Close()
	}
	if r.Results != nil {
		resultErr = r.Results.Close()
	}
	if r.ModelTurns != nil {
		modelTurnErr = r.ModelTurns.Close()
	}
	if r.Edge != nil {
		edgeErr = r.Edge.Close()
	}
	if serviceErr != nil || auditErr != nil || observabilityErr != nil || telemetryErr != nil || journalErr != nil || resultErr != nil || modelTurnErr != nil || edgeErr != nil {
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
	stateRoot := opts.StateRoot
	if stateRoot == "" {
		stateRoot = filepath.Join(primary, ".agent-memory", "state")
	}
	auditPath := opts.AuditPath
	if auditPath == "" {
		auditPath = filepath.Join(stateRoot, "logs", "audit.jsonl")
	}
	logger, err := audit.Open(auditPath)
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	observabilityConfig, err := resolveObservabilityConfig(opts.Observability, stateRoot)
	if err != nil {
		_ = logger.Close()
		return nil, err
	}
	observer, err := observability.Open(observabilityConfig, os.Stderr)
	if err != nil {
		_ = logger.Close()
		return nil, fmt.Errorf("opening observability sink: %w", err)
	}
	metrics, err := telemetry.Open(telemetry.Config{Path: filepath.Join(stateRoot, "telemetry", "metrics.db")})
	if err != nil {
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening telemetry store: %w", err)
	}
	observer.WithSink(metrics)
	journal, err := buildTaskJournal(opts.TaskRoot)
	if err != nil {
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, err
	}
	service, err := buildToolService(opts.Config, pol, logger, primary, opts.BrainRoot)
	if err != nil {
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, err
	}
	results, err := resultstore.Open(resultstore.Config{Root: filepath.Join(stateRoot, "results")})
	if err != nil {
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening result store: %w", err)
	}
	service = service.WithResultStore(results)
	modelTurns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(stateRoot, "model-turns")})
	if err != nil {
		_ = results.Close()
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening model turn store: %w", err)
	}
	edgeStore, err := edge.Open(edge.Config{Root: filepath.Join(stateRoot, "edge")})
	if err != nil {
		_ = modelTurns.Close()
		_ = results.Close()
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening edge identity store: %w", err)
	}
	return &appRuntime{
		Policy:      pol,
		Logger:      logger,
		Observer:    observer,
		Service:     service,
		Server:      mcpserver.NewWithObserver(service, observer).WithTaskJournal(journal).WithModelTurnStore(modelTurns).WithEdgeStore(edgeStore),
		Journal:     journal,
		PrimaryRoot: primary,
		AuditPath:   auditPath,
		StateRoot:   stateRoot,
		Telemetry:   metrics,
		Results:     results,
		ModelTurns:  modelTurns,
		Edge:        edgeStore,
	}, nil
}

func buildTaskJournal(root string) (*taskjournal.Journal, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	journal, err := taskjournal.Open(root)
	if err != nil {
		return nil, fmt.Errorf("initializing task journal: %w", err)
	}
	return journal, nil
}

func buildToolService(cfg config.Config, pol *policy.Policy, logger *audit.Logger, primary, brainRoot string) (*tools.Service, error) {
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
	brainStore, err := buildBrainStore(brainRoot, pol.Roots())
	if err != nil {
		return nil, err
	}
	if brainStore != nil {
		service = service.WithBrainStore(brainStore)
	}
	return service, nil
}

func buildBrainStore(root string, repositoryRoots []string) (*brainpkg.Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	for _, repositoryRoot := range repositoryRoots {
		if pathsOverlap(root, repositoryRoot) {
			return nil, errors.New("brain root must not overlap repository roots")
		}
	}
	store, err := brainpkg.OpenStoreWithClock(root, time.Now)
	if err != nil {
		return nil, fmt.Errorf("initializing brain store: %w", err)
	}
	cleanup := func() { _ = store.Close() }
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.InitializeGit(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("initializing brain history: %w", err)
	}
	if err := store.OpenIndex(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("opening brain index: %w", err)
	}
	if _, err := store.Reindex(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("reindexing brain: %w", err)
	}
	return store, nil
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
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
