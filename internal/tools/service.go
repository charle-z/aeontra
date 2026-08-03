// Package tools implements the L1 MCP tools. Every tool consults the policy before
// touching the filesystem or running a command, redacts content before returning
// it, and records an audit entry. Tools never re-implement security checks.
package tools

import (
	"context"
	"os"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
)

// maxReadBytes caps a single file read so a tool cannot be used to pull an
// unbounded amount of data in one call.
const maxReadBytes = 1 << 20 // 1 MiB

// Runner executes a program with an explicit argv inside dir (never via a shell)
// and returns combined stdout+stderr. Injectable for tests.
type Runner func(ctx context.Context, dir, prog string, args []string) (output string, err error)

// GitHubHTTPSRunner executes a fixed git argv against an already-validated GitHub
// HTTPS remote. The token is supplied out-of-band so it never enters the remote URL,
// command argv, audit record, or tool result.
type GitHubHTTPSRunner func(ctx context.Context, dir, prog string, args []string, token string) (output string, err error)

// Service is the backwards-compatible L1 facade. Its promoted methods are owned
// by focused capability services that all share one central security core.
type Service struct {
	*serviceCore
	*RepositoryCapability
	*GitCapability
	*SourceCapability
	*ManagedDeploymentCapability
	*ExecutionCapability
	*ResultCapability
	*BrainCapability
}

// NewService builds the shared core and every capability. root must be one of the
// policy's jail roots.
func NewService(pol *policy.Policy, log *audit.Logger, root string) *Service {
	core := &serviceCore{
		pol:   pol,
		log:   log,
		root:  root,
		run:   newExecRunner(pol.Roots()),
		plans: NewActionPlanStore(log),
	}
	source := &SourceCapability{serviceCore: core}
	git := &GitCapability{
		serviceCore:      core,
		SourceCapability: source,
		githubRun:        execGitHubHTTPSRunner,
	}
	repository := &RepositoryCapability{serviceCore: core, GitCapability: git}
	platform := &PlatformCapability{
		serviceCore:      core,
		SourceCapability: source,
		managedMCPToken: strings.TrimSpace(os.Getenv("MCP_DEVBOX_TOKEN")),
	}
	return &Service{
		serviceCore:                 core,
		RepositoryCapability:        repository,
		GitCapability:               git,
		SourceCapability:            source,
		ManagedDeploymentCapability: &ManagedDeploymentCapability{PlatformCapability: platform},
		ExecutionCapability: &ExecutionCapability{
			serviceCore: core,
			sandbox:     disabledSandboxRunner{},
			validation:  disabledValidationRunner{},
		},
		ResultCapability: &ResultCapability{serviceCore: core},
		BrainCapability:  &BrainCapability{serviceCore: core},
	}
}

// WithResultStore attaches the isolated bounded result store opened at startup.
func (s *Service) WithResultStore(store *resultstore.Store) *Service {
	s.ResultCapability.configureStore(store)
	return s
}

// WithActionPlanStore overrides the in-memory plan store for deterministic tests.
func (s *Service) WithActionPlanStore(store *ActionPlanStore) *Service {
	s.serviceCore.configureActionPlanStore(store)
	return s
}

// WithRunner overrides the command runner (tests).
func (s *Service) WithRunner(r Runner) *Service {
	s.serviceCore.configureRunner(r)
	// Tests and alternative runners retain control of the exact git invocation.
	s.GitCapability.configureRunner(r)
	return s
}

// WithSandboxRunner overrides the L3 sandbox runner (tests/future backends).
func (s *Service) WithSandboxRunner(r SandboxRunner) *Service {
	s.ExecutionCapability.configureSandbox(r)
	return s
}

// WithTestCommand sets the allowlisted command used by run_tests (e.g. {"go","test","./..."}).
func (s *Service) WithTestCommand(cmd []string) *Service {
	s.ExecutionCapability.configureTestCommand(cmd)
	return s
}

// WithCoolify sets the optional Coolify deploy client (nil disables coolify_deploy).
func (s *Service) WithCoolify(c *CoolifyClient) *Service {
	s.PlatformCapability.configureCoolify(c)
	return s
}

// WithGitHub sets the optional GitHub API client (nil disables GitHub tools).
func (s *Service) WithGitHub(c *GitHubClient) *Service {
	s.SourceCapability.configureGitHub(c)
	return s
}

// WithValidationRunner attaches the private fixed-profile runner. The public MCP
// still never receives a Docker socket or a general process executor.
func (s *Service) WithValidationRunner(r ValidationRunner) *Service {
	s.ExecutionCapability.configureValidationRunner(r)
	return s
}

// WithPrivilegedConfig applies immutable administrator startup configuration for
// closed privileged profiles. It is not exposed through MCP at runtime.
func (s *Service) WithPrivilegedConfig(cfg PrivilegedConfig) *Service {
	s.ExecutionCapability.configurePrivileged(cfg)
	return s
}

// WithBrainStore attaches one already-validated isolated Brain store at startup. The
// store root is never added to repository policy roots.
func (s *Service) WithBrainStore(store *brainpkg.Store) *Service {
	s.BrainCapability.configureStore(store)
	return s
}

// redact applies content-level secret scanning to any output before return.
func (s *serviceCore) redact(content string) string {
	out, _ := s.pol.Redact(content)
	return out
}

// summarize makes a short, safe args summary for the audit log.
func summarize(parts ...string) string {
	s := strings.Join(parts, " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
