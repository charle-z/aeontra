package app

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestParseServeOptionsObservabilityDefaultsToStderr(t *testing.T) {
	clearRuntimeEnv(t)
	opts, err := parseServeOptions([]string{"--root", t.TempDir()}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Observability.Mode != observability.ModeStderr {
		t.Fatalf("mode = %q", opts.Observability.Mode)
	}
	if opts.Observability.MaxBytes != observability.DefaultMaxBytes {
		t.Fatalf("max bytes = %d", opts.Observability.MaxBytes)
	}
	if opts.Observability.Path != "" {
		t.Fatalf("default path = %q", opts.Observability.Path)
	}
}

func TestParseServeOptionsObservabilityUsesEnvironment(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv(observabilityModeEnv, "file")
	t.Setenv(observabilityPathEnv, path)
	t.Setenv(observabilityMaxBytesEnv, "2097152")

	opts, err := parseServeOptions([]string{"--root", root}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Observability.Mode != observability.ModeFile || opts.Observability.Path != path || opts.Observability.MaxBytes != 2<<20 {
		t.Fatalf("observability = %+v", opts.Observability)
	}
}

func TestParseServeOptionsObservabilityFlagsWin(t *testing.T) {
	root := t.TempDir()
	flagPath := filepath.Join(t.TempDir(), "flag.jsonl")
	t.Setenv(observabilityModeEnv, "off")
	t.Setenv(observabilityPathEnv, filepath.Join(t.TempDir(), "env.jsonl"))
	t.Setenv(observabilityMaxBytesEnv, "4194304")

	opts, err := parseServeOptions([]string{
		"--root", root,
		"--observability", "both",
		"--observability-path", flagPath,
		"--observability-max-bytes", "8388608",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Observability.Mode != observability.ModeBoth || opts.Observability.Path != flagPath || opts.Observability.MaxBytes != 8<<20 {
		t.Fatalf("observability = %+v", opts.Observability)
	}
}

func TestParseServeOptionsRejectsInvalidObservability(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"--root", root, "--observability", "network"},
		{"--root", root, "--observability", "file", "--observability-path", "relative.jsonl"},
		{"--root", root, "--observability-max-bytes", "not-a-number"},
		{"--root", root, "--observability-max-bytes", "42"},
	} {
		if _, err := parseServeOptions(args, io.Discard); err == nil {
			t.Fatalf("args should fail: %v", args)
		}
	}
}

func TestResolveObservabilityPathUsesPrivateAgentMemoryDefault(t *testing.T) {
	root := t.TempDir()
	cfg, err := resolveObservabilityConfig(observability.Config{Mode: observability.ModeFile, MaxBytes: observability.DefaultMaxBytes}, root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "logs", "observability.jsonl")
	if cfg.Path != want {
		t.Fatalf("path = %q, want %q", cfg.Path, want)
	}
}
