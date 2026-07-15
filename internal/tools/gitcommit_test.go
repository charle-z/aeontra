package tools

import (
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

// configIdentity sets a local git identity so commits work in the test repo.
func configIdentity(t testing.TB, root string) {
	t.Helper()
	gitCmd(t, root, "config", "user.email", "t@t")
	gitCmd(t, root, "config", "user.name", "t")
}

func TestGitCommit_AllowCommits(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	configIdentity(t, root)
	write(t, root, "a.go", "package a\n")
	if _, err := svc.GitCommit("feat: add a", false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// The tree should now be clean (everything committed).
	out := gitCmd(t, root, "status", "--porcelain")
	if strings.TrimSpace(out) != "" {
		t.Errorf("working tree should be clean after commit, got: %q", out)
	}
	log := gitCmd(t, root, "log", "--oneline")
	if !strings.Contains(log, "feat: add a") {
		t.Errorf("commit message missing from log: %q", log)
	}
}

func TestGitCommit_ReadOnlyDenied(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	configIdentity(t, root)
	write(t, root, "a.go", "package a\n")
	if _, err := svc.GitCommit("msg", true); err == nil {
		t.Error("git_commit in read-only mode should be denied")
	}
}

func TestGitCommit_AskRequiresApproval(t *testing.T) {
	svc, root := initRepo(t, config.ModeAsk)
	configIdentity(t, root)
	write(t, root, "a.go", "package a\n")
	msg, err := svc.GitCommit("msg", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") {
		t.Errorf("ask mode should require approval: %q", msg)
	}
	// Nothing committed yet.
	if out := gitCmd(t, root, "status", "--porcelain"); strings.TrimSpace(out) == "" {
		t.Error("nothing should be committed before approval")
	}
	if _, err := svc.GitCommit("msg", true); err != nil {
		t.Fatalf("approved commit failed: %v", err)
	}
}

func TestGitCommit_EmptyMessageError(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	configIdentity(t, root)
	if _, err := svc.GitCommit("   ", true); err == nil {
		t.Error("empty commit message should error")
	}
}
