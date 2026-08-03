package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func brainRuntimeConfig(t *testing.T, repoRoot string) config.Config {
	t.Helper()
	cfg, err := config.New(config.Config{
		Roots:           []string{repoRoot},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git", "go"},
		SandboxBackend:  "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestParseServeOptionsLoadsOptionalBrainRootFromEnvironment(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	brainRoot := filepath.Join(t.TempDir(), "brain")
	t.Setenv(brainRootEnv, brainRoot)

	opts, err := parseServeOptions([]string{"--root", repoRoot}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.BrainRoot != filepath.Clean(brainRoot) {
		t.Fatalf("brain root=%q want=%q", opts.BrainRoot, filepath.Clean(brainRoot))
	}
}

func TestParseServeOptionsRejectsRelativeBrainRootWithoutEchoingValue(t *testing.T) {
	clearRuntimeEnv(t)
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	t.Setenv(brainRootEnv, "../"+secret)
	_, err := parseServeOptions([]string{"--root", t.TempDir()}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), brainRootEnv) || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
}

func TestBuildRuntimeLeavesBrainDisabledWhenRootIsUnset(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	runtime, err := buildRuntime(serveOptions{Config: brainRuntimeConfig(t, repoRoot)})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.Service.BrainCapability.Available() {
		t.Fatal("Brain unexpectedly available")
	}
	catalog, err := runtime.Server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ToolCount != 135 {
		t.Fatalf("tool count=%d want=135", catalog.ToolCount)
	}
	if _, err := runtime.Service.BrainContext(context.Background(), 1); !errors.Is(err, tools.ErrBrainNotConfigured) {
		t.Fatalf("disabled error=%v", err)
	}
}

func TestBuildRuntimeInitializesConfiguredBrainOutsideRepositoryRoots(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	brainRoot := filepath.Join(t.TempDir(), "brain")
	runtime, err := buildRuntime(serveOptions{
		Config:    brainRuntimeConfig(t, repoRoot),
		BrainRoot: brainRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if !runtime.Service.BrainCapability.Available() {
		t.Fatal("Brain capability is unavailable")
	}
	for _, relative := range []string{"", brainpkg.CuratedDir, brainpkg.WorkingDir, brainpkg.CacheDir, ".git"} {
		path := brainRoot
		if relative != "" {
			path = filepath.Join(brainRoot, relative)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s info=%v err=%v", relative, info, err)
		}
	}
	cache, err := os.Lstat(filepath.Join(brainRoot, brainpkg.CacheDir, brainpkg.IndexFileName))
	if err != nil || !cache.Mode().IsRegular() || cache.Mode().Perm() != 0o600 {
		t.Fatalf("cache=%v err=%v", cache, err)
	}
	status, err := runtime.Service.BrainIndex(context.Background(), "status")
	if err != nil || !status.Ready || status.NoteCount != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := runtime.Policy.CheckRead(filepath.Join(brainRoot, brainpkg.CuratedDir, "private-note.md")); !errors.Is(err, policy.ErrOutsideJail) {
		t.Fatalf("Brain entered repository roots: %v", err)
	}
}

func TestBuildRuntimeReindexesExistingMarkdownTruthAtStartup(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	brainRoot := filepath.Join(t.TempDir(), "brain")
	if err := os.Mkdir(brainRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{brainpkg.CuratedDir, brainpkg.WorkingDir, brainpkg.CacheDir} {
		if err := os.Mkdir(filepath.Join(brainRoot, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	source := fmt.Sprintf(`---
slug: startup-reference
title: Startup reference
type: reference
author: owner
created: %s
updated: %s
provenance: operator fixture
---

Runtime startup must reindex this searchable fixture.
`, now.Add(-time.Minute).Format(time.RFC3339), now.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(brainRoot, brainpkg.CuratedDir, "startup-reference.md"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := buildRuntime(serveOptions{Config: brainRuntimeConfig(t, repoRoot), BrainRoot: brainRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	results, err := runtime.Service.BrainSearch(context.Background(), "searchable fixture", 5)
	if err != nil || len(results) != 1 || results[0].Slug != "startup-reference" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	status, err := runtime.Service.BrainIndex(context.Background(), "status")
	if err != nil || status.NoteCount != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestBuildRuntimeRejectsBrainRootOverlapAndMalformedTruth(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	for name, brainRoot := range map[string]string{
		"equal":  repoRoot,
		"inside": filepath.Join(repoRoot, "brain"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildRuntime(serveOptions{Config: brainRuntimeConfig(t, repoRoot), BrainRoot: brainRoot})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "brain") || strings.Contains(err.Error(), brainRoot) {
				t.Fatalf("error=%v", err)
			}
		})
	}

	brainRoot := filepath.Join(t.TempDir(), "brain")
	if err := os.Mkdir(brainRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{brainpkg.CuratedDir, brainpkg.WorkingDir, brainpkg.CacheDir} {
		if err := os.Mkdir(filepath.Join(brainRoot, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(brainRoot, brainpkg.CuratedDir, "broken.md"), []byte("not frontmatter"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildRuntime(serveOptions{Config: brainRuntimeConfig(t, repoRoot), BrainRoot: brainRoot}); err == nil {
		t.Fatal("malformed Brain truth unexpectedly started")
	}
}

func TestBuildRuntimeRejectsBrainRepositoryWithRemote(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	brainRoot := filepath.Join(t.TempDir(), "brain")
	store, err := brainpkg.OpenStore(brainRoot, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	command := "[remote \"origin\"]\n\turl = https://example.invalid/private.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	configPath := filepath.Join(brainRoot, ".git", "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(data, []byte(command)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildRuntime(serveOptions{Config: brainRuntimeConfig(t, repoRoot), BrainRoot: brainRoot}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "brain") {
		t.Fatalf("error=%v", err)
	}
}
