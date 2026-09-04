package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/buildinfo"
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
	"github.com/charle-z/mcp-devbox/internal/workqueue"
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
	Sessions    *mcpserver.HTTPSessionStore
	WorkQueue   *workqueue.Store
}

func (r *appRuntime) Close() error {
	if r == nil {
		return nil
	}
	var serviceErr, auditErr, observabilityErr, telemetryErr, journalErr, resultErr, modelTurnErr, edgeErr, sessionErr, workQueueErr error
	if r.Server != nil {
		r.Server.StopProjectTaskCoordinator()
	}
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
	if r.Sessions != nil {
		sessionErr = r.Sessions.Close()
	}
	if r.WorkQueue != nil {
		workQueueErr = r.WorkQueue.Close()
	}
	if serviceErr != nil || auditErr != nil || observabilityErr != nil || telemetryErr != nil || journalErr != nil || resultErr != nil || modelTurnErr != nil || edgeErr != nil || sessionErr != nil || workQueueErr != nil {
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
		stateRoot, err = defaultRuntimeStateRoot(primary)
		if err != nil {
			return nil, err
		}
	}
	stateRoot = resolveRuntimePath(stateRoot)
	if err := validateRuntimeStateRoot(stateRoot, pol.Roots()); err != nil {
		return nil, err
	}
	auditPath := opts.AuditPath
	if auditPath == "" {
		auditPath = filepath.Join(stateRoot, "logs", "audit.jsonl")
	}
	if !filepath.IsAbs(auditPath) {
		return nil, errors.New("audit path must be absolute")
	}
	auditPath = resolveRuntimePath(auditPath)
	for _, root := range pol.Roots() {
		if pathsOverlap(auditPath, root) {
			return nil, errors.New("audit path must not overlap repository roots")
		}
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
	if observabilityConfig.Mode == observability.ModeFile || observabilityConfig.Mode == observability.ModeBoth {
		observabilityConfig.Path = resolveRuntimePath(observabilityConfig.Path)
		for _, root := range pol.Roots() {
			if pathsOverlap(observabilityConfig.Path, root) {
				_ = logger.Close()
				return nil, errors.New("observability path must not overlap repository roots")
			}
		}
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
	service, err := buildToolService(opts.Config, pol, logger, primary, opts.BrainRoot, stateRoot)
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
	workQueue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(stateRoot, "workqueue"), ControllerID: "mcp-devbox-control-plane"})
	if err != nil {
		_ = edgeStore.Close()
		_ = modelTurns.Close()
		_ = results.Close()
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening durable work queue: %w", err)
	}
	sessions, err := mcpserver.OpenHTTPSessionStore(filepath.Join(stateRoot, "mcp-sessions"))
	if err != nil {
		_ = workQueue.Close()
		_ = edgeStore.Close()
		_ = modelTurns.Close()
		_ = results.Close()
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, fmt.Errorf("opening MCP session store: %w", err)
	}

	server := mcpserver.NewWithObserver(service, observer).WithTaskJournal(journal).WithTelemetry(metrics).WithModelTurnStore(modelTurns).WithEdgeStore(edgeStore).WithWorkQueue(workQueue).WithConsoleStorageRoots(stateRoot, auditPath).WithHTTPSessionStore(sessions)
	catalog, err := server.CatalogInfo()
	if err != nil || edgeStore.SetExpectedOperationCompatibility(buildinfo.EdgeBundleProtocolVersion, catalog.Hash) != nil {
		_ = sessions.Close()
		_ = workQueue.Close()
		_ = edgeStore.Close()
		_ = modelTurns.Close()
		_ = results.Close()
		_ = service.BrainCapability.Close()
		_ = metrics.Close()
		_ = observer.Close()
		_ = logger.Close()
		return nil, errors.New("configuring edge operation compatibility")
	}

	return &appRuntime{
		Policy:      pol,
		Logger:      logger,
		Observer:    observer,
		Service:     service,
		Server:      server,
		Journal:     journal,
		PrimaryRoot: primary,
		AuditPath:   auditPath,
		StateRoot:   stateRoot,
		Telemetry:   metrics,
		Results:     results,
		ModelTurns:  modelTurns,
		Edge:        edgeStore,
		Sessions:    sessions,
		WorkQueue:   workQueue,
	}, nil
}

func defaultRuntimeStateRoot(primary string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(base) {
		return "", fmt.Errorf("resolving private default state root: configure %s explicitly", stateRootEnv)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(primary)))
	vendor := "aeontra"
	if runtime.GOOS == "windows" {
		vendor = "Aeontra"
	}
	return filepath.Join(base, vendor, "mcp-devbox", "state", fmt.Sprintf("%x", digest[:8])), nil
}

func validateRuntimeStateRoot(stateRoot string, repositoryRoots []string) error {
	cleaned := filepath.Clean(stateRoot)
	if !filepath.IsAbs(cleaned) || filepath.Dir(cleaned) == cleaned {
		return errors.New("state root must be an absolute non-root path")
	}
	for _, root := range repositoryRoots {
		if pathsOverlap(stateRoot, root) {
			return errors.New("state root must not overlap repository roots")
		}
	}
	return nil
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

func buildToolService(cfg config.Config, pol *policy.Policy, logger *audit.Logger, primary, brainRoot, stateRoot string) (*tools.Service, error) {
	maintainerProfile, err := loadMaintainerProfile()
	if err != nil {
		return nil, err
	}
	service := tools.NewService(pol, logger, primary).
		WithTestCommand(cfg.TestCommand).
		WithSandboxRunner(buildSandboxRunner(cfg, primary)).
		WithValidationRunner(buildValidationRunnerFromEnv()).
		WithMaintainerProfile(maintainerProfile)

	privileged, err := loadPrivilegedConfig()
	if err != nil {
		return nil, err
	}
	service = service.WithPrivilegedConfig(privileged)
	if coolify := buildCoolifyClientFromEnv(); coolify != nil {
		service = service.WithCoolify(coolify)
	}
	if github := buildGitHubClientFromEnv(); github != nil {
		service = service.WithGitHub(github.WithOSSToken(os.Getenv(githubOSSTokenEnv)))
	}
	brainStore, err := buildBrainStore(brainRoot, pol.Roots(), filepath.Join(stateRoot, "brain", "console-node.key"))
	if err != nil {
		return nil, err
	}
	if brainStore != nil {
		service = service.WithBrainStore(brainStore)
	}
	return service, nil
}

func loadMaintainerProfile() (string, error) {
	profile := strings.TrimSpace(os.Getenv(maintainerProfileEnv))
	switch profile {
	case "", tools.MaintainerProfileCharleZProduction:
		return profile, nil
	default:
		return "", fmt.Errorf("%s has an unsupported value", maintainerProfileEnv)
	}
}

func buildBrainStore(root string, repositoryRoots []string, consoleIdentityPath string) (*brainpkg.Store, error) {
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
	if err := store.ConfigureConsoleIdentity(consoleIdentityPath); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("initializing brain console identity: %w", err)
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

// resolveRuntimePath follows symlinks on the longest existing prefix and then
// rejoins any not-yet-created suffix. Private runtime paths are validated and
// opened using this canonical location so a symlink or junction cannot redirect
// state or audit data into a repository after the lexical overlap check.
func resolveRuntimePath(path string) string {
	path = filepath.Clean(path)
	remainder := ""
	current := path
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Clean(filepath.Join(resolved, remainder))
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
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
	if cfg.SandboxBackend != "private-rootless" {
		return runner
	}
	image := strings.ToLower(strings.TrimSpace(os.Getenv(sandboxImageEnv)))
	separator := strings.LastIndex(image, "@")
	if separator < 0 {
		return runner
	}
	return tools.NewPrivateSandboxRunner(tools.PrivateSandboxConfig{
		URL:           os.Getenv(sandboxRunnerURLEnv),
		Token:         os.Getenv(sandboxRunnerTokenEnv),
		WorkspaceID:   os.Getenv(sandboxWorkspaceIDEnv),
		WorkspaceRoot: primary,
		ImageDigest:   image[separator+1:],
	})
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
