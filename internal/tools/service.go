// Package tools implements the L1 MCP tools. Every tool consults the policy before
// touching the filesystem or running a command, redacts content before returning
// it, and records an audit entry. Tools never re-implement security checks.
package tools

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

// maxReadBytes caps a single file read so a tool cannot be used to pull an
// unbounded amount of data in one call.
const maxReadBytes = 1 << 20 // 1 MiB

// Runner executes a program with an explicit argv inside dir (never via a shell)
// and returns combined stdout+stderr. Injectable for tests.
type Runner func(ctx context.Context, dir, prog string, args []string) (output string, err error)

// Service is the L1 tool surface.
type Service struct {
	pol     *policy.Policy
	log     *audit.Logger
	root    string // primary project root: working dir for commands
	run     Runner
	sandbox SandboxRunner
	testCmd []string       // the single allowlisted test command (run_tests)
	coolify *CoolifyClient // optional; nil/unconfigured = coolify_deploy disabled
	github  *GitHubClient  // optional; nil/unconfigured = GitHub tools disabled
}

// NewService builds a Service. root must be one of the policy's jail roots.
func NewService(pol *policy.Policy, log *audit.Logger, root string) *Service {
	return &Service{pol: pol, log: log, root: root, run: execRunner, sandbox: disabledSandboxRunner{}}
}

// WithRunner overrides the command runner (tests).
func (s *Service) WithRunner(r Runner) *Service { s.run = r; return s }

// WithSandboxRunner overrides the L3 sandbox runner (tests/future backends).
func (s *Service) WithSandboxRunner(r SandboxRunner) *Service { s.sandbox = r; return s }

// WithTestCommand sets the allowlisted command used by run_tests (e.g. {"go","test","./..."}).
func (s *Service) WithTestCommand(cmd []string) *Service { s.testCmd = cmd; return s }

// WithCoolify sets the optional Coolify deploy client (nil disables coolify_deploy).
func (s *Service) WithCoolify(c *CoolifyClient) *Service { s.coolify = c; return s }

// WithGitHub sets the optional GitHub API client (nil disables GitHub tools).
func (s *Service) WithGitHub(c *GitHubClient) *Service { s.github = c; return s }

// execRunner is the default Runner: explicit argv, jailed working directory, a
// timeout, and combined output. It NEVER invokes a shell.
func execRunner(ctx context.Context, dir, prog string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// redact applies content-level secret scanning to any output before return.
func (s *Service) redact(content string) string {
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
