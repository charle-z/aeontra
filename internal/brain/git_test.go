package brain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestInitializeGitCreatesPrivateLocalRepositoryWithoutRemote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	gitInfo, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("git dir info=%v err=%v", gitInfo, err)
	}
	if gitInfo.Mode().Perm() != 0o700 {
		t.Fatalf("git dir permissions=%o", gitInfo.Mode().Perm())
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ignore) != "/.cache/\n" {
		t.Fatalf("gitignore=%q", ignore)
	}
	if remote := runGitTest(t, root, "remote"); remote != "" {
		t.Fatalf("unexpected remote=%q", remote)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("bootstrap commit count=%s", count)
	}
	if tree := runGitTest(t, root, "ls-tree", "--name-only", "HEAD"); tree != ".gitignore" {
		t.Fatalf("bootstrap tree=%q", tree)
	}
}

func TestInitializeGitIsIdempotentAndRejectsSymlinkGitDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count := runGitTest(t, root, "rev-list", "--count", "HEAD"); count != "1" {
		t.Fatalf("idempotent commit count=%s", count)
	}

	otherRoot := filepath.Join(t.TempDir(), "brain")
	other, err := OpenStore(otherRoot, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(otherRoot, ".git")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := other.InitializeGit(context.Background()); err == nil {
		t.Fatal("symlink .git unexpectedly accepted")
	}
}

func TestInitializeGitRejectsExistingRepoWithoutCacheIgnore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "cache") {
		t.Fatalf("missing cache ignore error=%v", err)
	}
}

func TestBrainGitNeverRunsHooks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "brain")
	store, err := OpenStore(root, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "hook-ran")
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho ran > \""+marker+"\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteAgent(context.Background(), AgentDraft{
		Slug:       "hook-test",
		Title:      "Hook test",
		Type:       TypeNote,
		Author:     "agent:chatgpt",
		Provenance: "controlled test",
		ReviewBy:   "2026-08-13",
		Body:       "Git plumbing must not run hooks.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hook marker exists: %v", err)
	}
}
