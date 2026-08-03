package tools

import (
	"context"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
)

// serviceCore centralizes the security and execution dependencies that every
// capability must share. Capabilities never own independent policy, audit, root, or
// action-plan state.
type serviceCore struct {
	pol   *policy.Policy
	log   *audit.Logger
	root  string
	run   Runner
	plans *ActionPlanStore
}

// RepositoryCapability owns repository, filesystem, memory, and notes behavior.
type RepositoryCapability struct {
	*serviceCore
	*GitCapability
}

// SourceCapability owns configured source-hosting API behavior.
type SourceCapability struct {
	*serviceCore
	github *GitHubClient
}

// GitCapability owns local/remote Git behavior and reuses the configured source
// capability for owner-bound GitHub authentication.
type GitCapability struct {
	*serviceCore
	*SourceCapability
	githubRun GitHubHTTPSRunner
}

// PlatformCapability owns deployment-platform behavior and reuses source-hosting
// configuration when application definitions reference GitHub repositories.
type PlatformCapability struct {
	*serviceCore
	*SourceCapability
	coolify                             *CoolifyClient
	managedFrontDoorProbe               func(context.Context, string, bool, string, string, string) error
	managedBackendIdentityFn            func(context.Context, string) (catalogrollout.Identity, error)
	managedFrontDoorSleepFn             func(time.Duration)
	managedFrontDoorExternalCoordinator bool
	managedMCPToken                     string
}

// ExecutionCapability owns process, sandbox, validation, and privileged-profile
// behavior.
type ExecutionCapability struct {
	*serviceCore
	sandbox    SandboxRunner
	testCmd    []string
	validation ValidationRunner
	privileged PrivilegedConfig
}

// ResultCapability owns access to bounded persisted tool output. The result store
// is state infrastructure, never a repository root or arbitrary filesystem view.
type ResultCapability struct {
	*serviceCore
	mu    sync.RWMutex
	store *resultstore.Store
}

func (c *ResultCapability) configureStore(store *resultstore.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
}

func (c *serviceCore) configureActionPlanStore(store *ActionPlanStore) {
	c.plans = store
}

func (c *serviceCore) configureRunner(r Runner) {
	c.run = r
}

func (c *GitCapability) configureRunner(r Runner) {
	c.githubRun = func(ctx context.Context, dir, prog string, args []string, _ string) (string, error) {
		return r(ctx, dir, prog, args)
	}
}

func (c *SourceCapability) configureGitHub(client *GitHubClient) {
	c.github = client
}

func (c *PlatformCapability) configureCoolify(client *CoolifyClient) {
	c.coolify = client
}

func (c *ExecutionCapability) configureSandbox(runner SandboxRunner) {
	c.sandbox = runner
}

func (c *ExecutionCapability) configureTestCommand(command []string) {
	c.testCmd = command
}

func (c *ExecutionCapability) configureValidationRunner(runner ValidationRunner) {
	if runner == nil {
		runner = disabledValidationRunner{}
	}
	c.validation = runner
}

func (c *ExecutionCapability) configurePrivileged(config PrivilegedConfig) {
	c.privileged = normalizePrivilegedConfig(config)
}
