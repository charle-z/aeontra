package app

import (
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestParseServeOptionsFlagsWinAndRootsRemainRepeatable(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	t.Setenv(allowCmdEnv, "env-program")
	t.Setenv(testCmdEnv, "env-test --run")
	t.Setenv(sandboxEnv, "gvisor")

	opts, err := parseServeOptions([]string{
		"--root", rootA + "," + rootB,
		"--mode", "ask",
		"--allow-cmd", "git,go",
		"--test-cmd", "go test ./...",
		"--audit", filepath.Join(rootA, "audit.jsonl"),
		"--http", ":8765",
		"--http-token", "flag-token",
		"--sandbox", "docker",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Config.Mode != config.ModeAsk {
		t.Fatalf("mode = %q", opts.Config.Mode)
	}
	if !reflect.DeepEqual(opts.Config.Roots, []string{filepath.Clean(rootA), filepath.Clean(rootB)}) {
		t.Fatalf("roots = %#v", opts.Config.Roots)
	}
	if !reflect.DeepEqual(opts.Config.AllowedCommands, []string{"git", "go"}) {
		t.Fatalf("allowed commands = %#v", opts.Config.AllowedCommands)
	}
	if !reflect.DeepEqual(opts.Config.TestCommand, []string{"go", "test", "./..."}) {
		t.Fatalf("test command = %#v", opts.Config.TestCommand)
	}
	if opts.Config.SandboxBackend != "docker" {
		t.Fatalf("sandbox = %q", opts.Config.SandboxBackend)
	}
	if opts.AuditPath != filepath.Join(rootA, "audit.jsonl") || opts.HTTPAddr != ":8765" || opts.HTTPToken != "flag-token" {
		t.Fatalf("transport/audit options = %#v", opts)
	}
}

func TestParseServeOptionsUsesExistingEnvironmentNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv(allowCmdEnv, "git,node")
	t.Setenv(testCmdEnv, "node test.js")
	t.Setenv(sandboxEnv, "gvisor")
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv(stateRootEnv, stateRoot)

	opts, err := parseServeOptions([]string{"--root", root}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts.Config.AllowedCommands, []string{"git", "node"}) {
		t.Fatalf("allowed commands = %#v", opts.Config.AllowedCommands)
	}
	if !reflect.DeepEqual(opts.Config.TestCommand, []string{"node", "test.js"}) {
		t.Fatalf("test command = %#v", opts.Config.TestCommand)
	}
	if opts.Config.SandboxBackend != "gvisor" {
		t.Fatalf("sandbox = %q", opts.Config.SandboxBackend)
	}
	if opts.StateRoot != filepath.Clean(stateRoot) {
		t.Fatalf("state root = %q", opts.StateRoot)
	}
}

func TestParseServeOptionsRejectsRelativeStateRoot(t *testing.T) {
	t.Setenv(stateRootEnv, "relative-state")
	if _, err := parseServeOptions([]string{"--root", t.TempDir()}, io.Discard); err == nil || !strings.Contains(err.Error(), stateRootEnv) {
		t.Fatalf("error = %v", err)
	}
}

func TestParseServeOptionsAddsTestProgramToDefaultAllowlist(t *testing.T) {
	root := t.TempDir()
	t.Setenv(testCmdEnv, "node test.js")

	opts, err := parseServeOptions([]string{"--root", root}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts.Config.AllowedCommands, []string{"git", "go", "ls", "cat", "node"}) {
		t.Fatalf("allowed commands = %#v", opts.Config.AllowedCommands)
	}
}

func TestParseServeOptionsRequiresRoot(t *testing.T) {
	_, err := parseServeOptions(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "at least one --root is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseServeOptionsRejectsUnknownMode(t *testing.T) {
	_, err := parseServeOptions([]string{"--root", t.TempDir(), "--mode", "alow"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("parseServeOptions(mode=alow) = %v, want unknown mode error", err)
	}
}
