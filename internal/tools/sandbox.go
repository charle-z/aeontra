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
	Available     bool
	Backend       string
	DefaultEgress string
	FreeTerminal  bool
	Notes         []string
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

// SandboxStatus reports whether an L3 backend is configured. This is diagnostic
// only; it does not grant extra command capability.
func (s *Service) SandboxStatus() string {
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
	if len(status.Notes) > 0 {
		b.WriteString("notes:\n")
		for _, note := range status.Notes {
			fmt.Fprintf(&b, "- %s\n", note)
		}
	}
	return b.String()
}
