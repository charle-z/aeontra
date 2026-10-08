package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// SandboxRunner is the future L3 execution boundary. It is deliberately separate
// from Runner: plain exec remains L1, while broad commands require a real sandbox
// backend plus policy and approval checks before they reach this interface.
type SandboxRunner interface {
	Status(ctx context.Context) SandboxStatusInfo
	Run(ctx context.Context, req SandboxRunRequest) (SandboxRunResult, error)
}

type SandboxRunRequest struct {
	Dir            string
	Argv           []string
	EnvAllowlist   map[string]string
	NetworkProfile string
	Timeout        time.Duration
}

type SandboxRunResult struct {
	ExitCode       int
	Stdout         string
	Stderr         string
	Duration       time.Duration
	SandboxBackend string
	EgressProfile  string
}

type SandboxStatusInfo struct {
	Available       bool
	Backend         string
	DefaultEgress   string
	FreeTerminal    bool
	ContainerReady  bool
	ExecReady       bool
	FilesystemReady bool
	GitReady        bool
	NetworkPolicy   string
	ToolchainState  string
	Notes           []string
}

// NewSandboxRunner returns the sandbox runner for a configured backend name
// (already validated by config.New). "" or "none" is fully disabled. A known backend
// name is "pending": plumbed and visible in sandbox_status but still UNAVAILABLE —
// Run errors and no broad command execution is granted until the real L3 backend
// lands and its adversarial egress/escape tests pass.
func NewSandboxRunner(backend string) SandboxRunner {
	b := strings.ToLower(strings.TrimSpace(backend))
	if b == "" || b == "none" {
		return disabledSandboxRunner{}
	}
	return pendingSandboxRunner{backend: b}
}

type disabledSandboxRunner struct{}

func (disabledSandboxRunner) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{
		Available:     false,
		Backend:       "none",
		DefaultEgress: "deny",
		FreeTerminal:  false,
		Notes: []string{
			"L3 sandbox backend is not configured",
			"no Docker socket in the public MCP container",
			"run_command remains allowlist-only; no free terminal before L3",
		},
	}
}

func (disabledSandboxRunner) Run(context.Context, SandboxRunRequest) (SandboxRunResult, error) {
	return SandboxRunResult{}, errors.New("L3 sandbox backend is not configured")
}

// pendingSandboxRunner represents a backend named in config but not yet implemented.
// It is visible in status but grants no execution capability (Run always errors).
type pendingSandboxRunner struct{ backend string }

func (p pendingSandboxRunner) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{
		Available:     false,
		Backend:       p.backend,
		DefaultEgress: "deny",
		FreeTerminal:  false,
		Notes: []string{
			fmt.Sprintf("sandbox backend %q is configured but NOT yet implemented (L3 pending)", p.backend),
			"execution stays L1 allowlist-only; no free terminal before L3",
			"no Docker socket is mounted into the public MCP container",
			"broad commands remain unavailable until adversarial egress/escape tests pass",
		},
	}
}

func (p pendingSandboxRunner) Run(context.Context, SandboxRunRequest) (SandboxRunResult, error) {
	return SandboxRunResult{}, fmt.Errorf("sandbox backend %q not yet implemented (L3 pending)", p.backend)
}

// SandboxExec runs an ARBITRARY command inside the L3 sandbox. Unlike run_command it
// is NOT allowlist-limited: the sandbox (network-denied, read-only rootfs, workspace-
// only, resource-limited) is what contains it, so shells and arbitrary tools are
// permitted — but ONLY when a real sandbox backend is available. It is mode-gated
// (read-only and ask deny; allow runs), audited, and its combined
// output is redacted before return. This is "broad execution, contained": it never
// grants the model a general-purpose control plane over the host.
func (s *ExecutionCapability) SandboxExec(argv []string) (string, error) {
	return s.SandboxExecIn(argv, "")
}

// SandboxExecIn is SandboxExec with an explicit, jailed working directory. A
// multi-repository root requires this selection so the private executor mounts
// only one direct workspace rather than the complete repository jail.
func (s *ExecutionCapability) SandboxExecIn(argv []string, cwd string) (string, error) {
	sp := s.log.Start("sandbox_exec")
	if len(argv) == 0 {
		err := fmt.Errorf("command is required")
		sp.Finish(audit.Error, "sandbox_exec", nil, err)
		return "", err
	}
	// No broad execution without containment.
	if !s.sandbox.Status(context.Background()).Available {
		err := fmt.Errorf("sandbox_exec requires an attested private L3 executor; broad execution is disabled without containment")
		sp.Finish(audit.Deny, summarize(argv...), nil, err)
		return "", err
	}
	if err := s.pol.CheckContainedExecution(); err != nil {
		sp.Finish(audit.Deny, summarize(argv...), nil, err)
		return "", err
	}

	dir, err := s.workdir(cwd)
	if err != nil {
		sp.Finish(audit.Deny, summarize(argv...), nil, err)
		return "", err
	}
	res, runErr := s.sandbox.Run(context.Background(), SandboxRunRequest{Dir: dir, Argv: argv})
	combined := strings.TrimRight(res.Stdout+"\n"+res.Stderr, "\n")
	out := s.redact(combined)
	if runErr != nil {
		sp.Finish(audit.Error, summarize(argv...), []string{dir}, runErr)
		return out, fmt.Errorf("sandbox exec failed: %w", runErr)
	}
	sp.Finish(audit.Allow, summarize(argv...), []string{dir}, nil)
	return fmt.Sprintf("[exit %d, backend %s, egress %s]\n%s", res.ExitCode, res.SandboxBackend, res.EgressProfile, out), nil
}

// SandboxStatus reports whether an L3 backend is configured. This is diagnostic
// only; it does not grant extra command capability.
func (s *ExecutionCapability) SandboxStatus() string {
	sp := s.log.Start("sandbox_status")
	status := s.sandbox.Status(context.Background())
	sp.Finish(audit.Allow, "sandbox status", nil, nil)
	return formatSandboxStatus(status)
}

func formatSandboxStatus(status SandboxStatusInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "available: %t\n", status.Available)
	fmt.Fprintf(&b, "backend: %s\n", status.Backend)
	fmt.Fprintf(&b, "default_egress: %s\n", status.DefaultEgress)
	fmt.Fprintf(&b, "free_terminal: %t\n", status.FreeTerminal)
	fmt.Fprintf(&b, "container_ready: %t\n", status.ContainerReady)
	fmt.Fprintf(&b, "exec_ready: %t\n", status.ExecReady)
	fmt.Fprintf(&b, "filesystem_ready: %t\n", status.FilesystemReady)
	fmt.Fprintf(&b, "git_ready: %t\n", status.GitReady)
	fmt.Fprintf(&b, "network_policy: %s\n", status.NetworkPolicy)
	fmt.Fprintf(&b, "toolchain: %s\n", status.ToolchainState)
	if len(status.Notes) > 0 {
		b.WriteString("notes:\n")
		for _, note := range status.Notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
	}
	return b.String()
}
