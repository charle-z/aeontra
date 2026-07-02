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
