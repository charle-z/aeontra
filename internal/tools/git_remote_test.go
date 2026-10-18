package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestGitClone_AskRequiresApproval(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	var ran bool
	svc.WithRunner(func(context.Context, string, string, []string) (string, error) {
		ran = true
		return "should not run", nil
	})

	out, err := svc.GitClone("https://github.com/charle-z/demo.git", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask mode should require approval, got %q", out)
	}
	if ran {
		t.Fatal("git clone must not run before approval")
	}
}

func TestGitClone_DeniesTargetEscape(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("should not run", nil))

	if _, err := svc.GitClone("https://github.com/charle-z/demo.git", "../escape", true); err == nil {
		t.Fatal("git_clone target must not escape the workspace jail")
	}
}

func TestGitClone_RejectsTokenInURL(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("should not run", nil))

	if _, err := svc.GitClone("https://ghp_0123456789abcdefghijklmnopqrstuvwxyz@github.com/charle-z/demo.git", "", true); err == nil {
		t.Fatal("git_clone must reject URLs with embedded credentials")
	}
}

func TestGitClone_RunsControlledClone(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	var gotDir, gotProg string
	var gotArgs []string
	svc.WithRunner(func(_ context.Context, dir, prog string, args []string) (string, error) {
		gotDir, gotProg, gotArgs = dir, prog, args
		return "cloned", nil
	})

	out, err := svc.GitClone("https://github.com/charle-z/demo.git", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != root || gotProg != "git" {
		t.Fatalf("clone ran in %q with %q, want root %q git", gotDir, gotProg, root)
	}
	want := []string{"clone", "https://github.com/charle-z/demo.git", "demo"}
	if strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("clone args = %#v, want %#v", gotArgs, want)
	}
	if strings.Contains(out, "ghp_") {
		t.Fatalf("clone output should be redacted: %q", out)
	}
}

func TestGitPush_AskRequiresApproval(t *testing.T) {
	svc, root := newTestService(t, config.ModeAsk)
	repo := filepath.Join(root, "demo")
	write(t, repo, "README.md", "# demo\n")
	calls := 0
	svc.WithRunner(func(_ context.Context, _ string, _ string, args []string) (string, error) {
		calls++
		if len(args) > 0 && args[0] == "branch" {
			return "main\n", nil
		}
		return "should not push", nil
	})

	out, err := svc.GitPush("demo", "origin", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask mode should require approval, got %q", out)
	}
	if calls != 1 {
		t.Fatalf("only branch discovery should run before approval, calls=%d", calls)
	}
}

func TestGitPush_DeniesRepoOutsideWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	outside := filepath.Join(filepath.Dir(root), "outside")
	write(t, outside, "README.md", "# outside\n")

	if _, err := svc.GitPush(outside, "origin", "main", true); err == nil {
		t.Fatal("git_push repo selector must not escape the workspace jail")
	}
}

func TestGitPush_RejectsOptionLikeRemoteOrBranch(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	write(t, filepath.Join(root, "demo"), "README.md", "# demo\n")

	if _, err := svc.GitPush("demo", "--force", "main", true); err == nil {
		t.Fatal("git_push must reject option-like remotes")
	}
	if _, err := svc.GitPush("demo", "origin", "--force", true); err == nil {
		t.Fatal("git_push must reject option-like branches")
	}
}

func TestGitPush_RunsControlledPush(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "demo")
	write(t, repo, "README.md", "# demo\n")
	var gotDir, gotProg string
	var gotArgs []string
	svc.WithRunner(func(_ context.Context, dir, prog string, args []string) (string, error) {
		gotDir, gotProg, gotArgs = dir, prog, args
		return "pushed", nil
	})

	if _, err := svc.GitPush("demo", "origin", "main", false); err != nil {
		t.Fatal(err)
	}
	want := []string{"push", "origin", "main"}
	if gotDir != repo || gotProg != "git" || strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("push ran dir=%q prog=%q args=%#v, want dir=%q prog=git args=%#v", gotDir, gotProg, gotArgs, repo, want)
	}
}
