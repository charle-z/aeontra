package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestRuntimeCloseClosesAttachedBrainCapability(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{repoRoot},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
		SandboxBackend:  "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildRuntime(serveOptions{
		Config:    cfg,
		AuditPath: filepath.Join(repoRoot, "audit.jsonl"),
	})
	if err != nil {
		t.Fatal(err)
	}
	brainRoot := filepath.Join(t.TempDir(), "brain")
	store, err := brainpkg.OpenStore(brainRoot, time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime.Service.WithBrainStore(store)

	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.Service.BrainCapability.Available() {
		t.Fatal("Brain capability remained available after runtime close")
	}
	if _, err := store.IndexStatus(context.Background()); err == nil {
		t.Fatal("Brain store remained open after runtime close")
	}
}
