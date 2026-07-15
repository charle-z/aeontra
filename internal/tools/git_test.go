package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func gitCmd(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func initRepo(t testing.TB, mode config.Mode) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	svc, root := newTestService(t, mode)
	gitCmd(t, root, "init", "-q")
	return svc, root
}

func TestGitStatus_Works(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	write(t, root, "a.go", "package a\n")
	out, err := svc.GitStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("status should mention untracked a.go: %q", out)
	}
}

func TestGitStatus_WorksInSelectedRepoUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	repo := filepath.Join(root, "mcp-devbox")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")
	write(t, repo, "a.go", "package a\n")

	out, err := svc.GitStatus("mcp-devbox")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("status should mention subrepo file: %q", out)
	}
}

func TestGitStatus_DeniesRepoOutsideWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	outside := filepath.Join(filepath.Dir(root), "outside-repo")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, outside, "init", "-q")

	if _, err := svc.GitStatus(outside); err == nil {
		t.Fatal("git_status repo selector must not escape the workspace jail")
	}
}

func TestGitDiff_Works(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	write(t, root, "a.txt", "one\n")
	gitCmd(t, root, "add", "a.txt")
	gitCmd(t, root, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := svc.GitDiff()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+two") {
		t.Errorf("diff should show added line: %q", out)
	}
}

func TestGitDiff_WorksInSelectedRepoUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	repo := filepath.Join(root, "mcp-devbox")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")
	write(t, repo, "a.txt", "one\n")
	gitCmd(t, repo, "add", "a.txt")
	gitCmd(t, repo, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := svc.GitDiffIn("mcp-devbox")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+two") {
		t.Errorf("diff should show added line in subrepo: %q", out)
	}
}

func TestGitCommit_CommitsSelectedRepoUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")
	configIdentity(t, repo)
	write(t, repo, "a.go", "package a\n")

	if _, err := svc.GitCommitIn("demo", "feat: add a", false); err != nil {
		t.Fatalf("commit selected repo: %v", err)
	}
	if out := gitCmd(t, repo, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Fatalf("selected repo should be clean after commit, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(root, "a.go")); !os.IsNotExist(err) {
		t.Fatalf("commit should not create or stage files relative to root, stat err=%v", err)
	}
}

func makePatch(t *testing.T, root, file, oldC, newC string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, file), []byte(oldC), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", file)
	gitCmd(t, root, "commit", "-qm", "base")
	if err := os.WriteFile(filepath.Join(root, file), []byte(newC), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := gitCmd(t, root, "diff")
	gitCmd(t, root, "checkout", "--", file) // revert; ApplyPatch will redo it
	return patch
}

func TestApplyPatch_AppliesWhenAllowed(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	patch := makePatch(t, root, "a.txt", "one\n", "one\ntwo\n")
	msg, err := svc.ApplyPatch(patch, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(msg, "Applied") {
		t.Errorf("unexpected message: %q", msg)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if normalizeEOL(string(got)) != "one\ntwo\n" {
		t.Errorf("file not patched: %q", got)
	}
}

func TestApplyPatch_AppliesInSelectedRepoUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")
	patch := makePatch(t, repo, "a.txt", "one\n", "one\ntwo\n")

	if _, err := svc.ApplyPatchIn("demo", patch, false); err != nil {
		t.Fatalf("apply selected repo: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	if normalizeEOL(string(got)) != "one\ntwo\n" {
		t.Errorf("selected repo file not patched: %q", got)
	}
}

// normalizeEOL collapses CRLF to LF so tests are robust to git's autocrlf on Windows.
func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func TestApplyPatch_AskRequiresApproval(t *testing.T) {
	svc, root := initRepo(t, config.ModeAsk)
	patch := makePatch(t, root, "a.txt", "one\n", "one\ntwo\n")
	msg, err := svc.ApplyPatch(patch, false) // not approved
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") {
		t.Errorf("ask mode should require approval: %q", msg)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if normalizeEOL(string(got)) != "one\n" {
		t.Errorf("file should NOT be modified before approval: %q", got)
	}
	// Now approve.
	if _, err := svc.ApplyPatch(patch, true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "a.txt"))
	if normalizeEOL(string(got)) != "one\ntwo\n" {
		t.Errorf("file should be patched after approval: %q", got)
	}
}

func TestApplyPatch_ReadOnlyDenied(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	patch := makePatch(t, root, "a.txt", "one\n", "one\ntwo\n")
	if _, err := svc.ApplyPatch(patch, true); err == nil {
		t.Error("apply in read-only mode should be denied")
	}
}

func TestApplyPatch_DeniesSecretTarget(t *testing.T) {
	svc, _ := initRepo(t, config.ModeAllow)
	patch := "diff --git a/.env b/.env\n--- a/.env\n+++ b/.env\n@@ -0,0 +1 @@\n+SECRET=x\n"
	if _, err := svc.ApplyPatch(patch, true); err == nil {
		t.Error("patch targeting .env should be denied")
	}
}

func TestApplyPatch_DeniesEscapeTarget(t *testing.T) {
	svc, _ := initRepo(t, config.ModeAllow)
	patch := "diff --git a/../escape.txt b/../escape.txt\n--- a/../escape.txt\n+++ b/../escape.txt\n@@ -0,0 +1 @@\n+pwn\n"
	if _, err := svc.ApplyPatch(patch, true); err == nil {
		t.Error("patch escaping the jail should be denied")
	}
}

func TestApplyPatch_NoTargets(t *testing.T) {
	svc, _ := initRepo(t, config.ModeAllow)
	if _, err := svc.ApplyPatch("not a patch", true); err == nil {
		t.Error("patch with no targets should error")
	}
}

// Sanity: the default execRunner is wired (used indirectly above), but also
// exercise it directly for coverage of the jailed working dir.
func TestExecRunner_RunsInDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	out, err := newExecRunner([]string{dir})(context.Background(), dir, "git", []string{"version"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "git version") {
		t.Errorf("unexpected: %q", out)
	}
}
