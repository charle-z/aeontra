package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitObjectDirectorySupportsLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	worktree := filepath.Join(root, "worktree")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repository, "init", "-q")
	write(t, repository, "README.md", "fixture\n")
	gitCmd(t, repository, "add", "README.md")
	gitCmd(t, repository, "commit", "-qm", "fixture")
	gitCmd(t, repository, "worktree", "add", "-q", "-b", "fixture-worktree", worktree)

	objects, err := gitObjectDirectory(worktree)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repository, ".git", "objects")
	if !strings.EqualFold(filepath.Clean(objects), filepath.Clean(want)) {
		t.Fatalf("objects=%q want=%q", objects, want)
	}
}

func TestValidateGitHubHTTPSArgumentsRejectsCallerRemoteNames(t *testing.T) {
	remote := "https://github.com/acme/demo.git"
	head := strings.Repeat("a", 40)
	for _, args := range [][]string{
		{"ls-remote", "--heads", remote, "refs/heads/main"},
		{"push", "--porcelain", remote, head + ":refs/heads/main"},
	} {
		if _, _, err := validateGitHubHTTPSArguments(args); err != nil {
			t.Fatalf("valid operation rejected: %q: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"ls-remote", "--heads", "origin", "refs/heads/main"},
		{"push", "--force", remote, head + ":refs/heads/main"},
		{"push", "--porcelain", "https://attacker.example/demo.git", head + ":refs/heads/main"},
	} {
		if _, _, err := validateGitHubHTTPSArguments(args); err == nil {
			t.Fatalf("unsafe operation accepted: %q", args)
		}
	}
}
