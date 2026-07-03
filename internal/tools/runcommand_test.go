package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func TestRunCommand_AllowRuns(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("on branch main\n", nil))
	out, err := svc.RunCommand("git", []string{"status"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "branch main") {
		t.Errorf("unexpected: %q", out)
	}
}

func TestRunCommand_RunsInSelectedCWDUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "mcp-devbox")
	write(t, repo, "README.md", "# repo\n")
	var gotDir string
	svc.WithRunner(func(ctx context.Context, dir, prog string, args []string) (string, error) {
		gotDir = dir
		return "ok", nil
	})

	if _, err := svc.RunCommandIn("git", []string{"status"}, false, "mcp-devbox"); err != nil {
		t.Fatal(err)
	}
	if gotDir != repo {
		t.Fatalf("runner dir = %q, want %q", gotDir, repo)
	}
}

func TestRunCommand_DeniesCWDOutsideWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	outside := filepath.Join(filepath.Dir(root), "outside-cwd")
	write(t, outside, "x.txt", "x")
	svc.WithRunner(fakeRunner("should not run", nil))

	if _, err := svc.RunCommandIn("git", []string{"status"}, true, outside); err == nil {
		t.Fatal("run_command cwd must not escape the workspace jail")
	}
}

func TestRunCommand_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithRunner(fakeRunner("x", nil))
	if _, err := svc.RunCommand("git", []string{"status"}, true); err == nil {
		t.Error("run_command in read-only should be denied")
	}
}

func TestRunCommand_AskRequiresApproval(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithRunner(fakeRunner("ran", nil))
	msg, err := svc.RunCommand("git", []string{"status"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") {
		t.Errorf("ask mode should require approval: %q", msg)
	}
}

func TestRunCommand_NonAllowlistedDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("x", nil))
	if _, err := svc.RunCommand("python", []string{"-c", "print(1)"}, true); !errors.Is(err, policy.ErrCommandNotAllowed) {
		t.Errorf("non-allowlisted command should be denied, got %v", err)
	}
}

func TestRunCommand_InjectionDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("x", nil))
	if _, err := svc.RunCommand("git", []string{"status; rm -rf /"}, true); !errors.Is(err, policy.ErrCommandInjection) {
		t.Errorf("injected metacharacters should be denied, got %v", err)
	}
}

func TestRunCommand_DestructiveDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("x", nil))
	// git is allowlisted, but push is blocked as destructive.
	if _, err := svc.RunCommand("git", []string{"push", "--force"}, true); !errors.Is(err, policy.ErrCommandDestructive) {
		t.Errorf("destructive git should be denied, got %v", err)
	}
}

func TestRunCommand_RedactsOutput(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithRunner(fakeRunner("leaked ghp_0123456789abcdefghijklmnopqrstuvwxyz here", nil))
	out, err := svc.RunCommand("git", []string{"log"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("run_command leaked a secret: %q", out)
	}
}
