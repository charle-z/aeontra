// Package config holds the immutable, secure-by-default configuration for the
// mcp-devbox daemon. It is loaded once at startup. Nothing in this process — and
// in particular no MCP tool driven by the agent — may mutate the policy after load
// (constitution Article I.8).
package config

import (
	"errors"
	"path/filepath"
	"strings"
)

// Mode is the effective access posture for writes and command execution.
type Mode string

const (
	// ModeReadOnly is the default: no writes, no command execution.
	ModeReadOnly Mode = "read-only"
	// ModeAsk allows writes/commands but routes risky actions through an approval
	// gate (the tool returns "approval required" instead of executing).
	ModeAsk Mode = "ask"
	// ModeAllow executes allowlisted writes/commands without prompting. Reserved
	// for trusted local automation; never the default.
	ModeAllow Mode = "allow"
)

// Config is the root configuration. Fields are unexported-by-intent through the
// constructor: callers build a Config via New and then treat it as read-only.
type Config struct {
	// Roots are the absolute, symlink-resolved project directories that form the
	// filesystem + command-execution jail. Operations must resolve inside one.
	Roots []string
	// Mode is the write/command posture. Defaults to ModeReadOnly.
	Mode Mode
	// AllowedCommands is the per-project command allowlist (program basenames).
	AllowedCommands []string
	// TestCommand is the single allowlisted command used by run_tests.
	TestCommand []string
	// AuditPath is where the append-only audit log is written.
	AuditPath string
	// SandboxBackend names the (future) L3 execution backend: "none" (default,
	// disabled) or a known name (docker/nsjail/gvisor). Known names are plumbed and
	// visible in sandbox_status but NOT yet implemented — configuring one does not
	// enable broad command execution (L3 pending).
	SandboxBackend string
}

// KnownSandboxBackends are the L3 backends the config accepts. They are not yet
// implemented; naming one only wires status/plumbing.
var KnownSandboxBackends = []string{"docker", "nsjail", "gvisor"}

var (
	// ErrNoRoots is returned when no project root is configured.
	ErrNoRoots = errors.New("config: at least one project root is required")
	// ErrRootNotAbsolute is returned when a root is not an absolute path.
	ErrRootNotAbsolute = errors.New("config: project root must be an absolute path")
	// ErrUnknownSandboxBackend is returned for an unrecognized sandbox backend.
	ErrUnknownSandboxBackend = errors.New("config: unknown sandbox backend (use none/docker/nsjail/gvisor)")
)

// SecureDefaults returns a Config pre-populated with the secure-by-default posture:
// read-only, a conservative command allowlist, and no test command. Callers set
// Roots (and optionally relax Mode) before use.
func SecureDefaults() Config {
	return Config{
		Mode: ModeReadOnly,
		// Conservative, non-destructive, read-oriented programs only.
		AllowedCommands: []string{"git", "go", "ls", "cat"},
	}
}

// New validates and finalizes a Config. Roots are cleaned and required to be
// absolute. It does NOT resolve symlinks here (that is the jail's job at check
// time, against the live filesystem); it only enforces structural invariants.
func New(c Config) (Config, error) {
	if len(c.Roots) == 0 {
		return Config{}, ErrNoRoots
	}
	cleaned := make([]string, 0, len(c.Roots))
	for _, r := range c.Roots {
		if !filepath.IsAbs(r) {
			return Config{}, ErrRootNotAbsolute
		}
		cleaned = append(cleaned, filepath.Clean(r))
	}
	c.Roots = cleaned
	if c.Mode == "" {
		c.Mode = ModeReadOnly
	}

	// Sandbox backend: empty -> "none" (disabled); otherwise must be a known name.
	switch b := strings.ToLower(strings.TrimSpace(c.SandboxBackend)); b {
	case "", "none":
		c.SandboxBackend = "none"
	default:
		known := false
		for _, k := range KnownSandboxBackends {
			if b == k {
				known = true
				break
			}
		}
		if !known {
			return Config{}, ErrUnknownSandboxBackend
		}
		c.SandboxBackend = b
	}
	return c, nil
}
