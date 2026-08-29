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
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{ExitCode: 0, Stdout: "on branch main\n"}})
	out, err := svc.RunCommand("git", []string{"status"})
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
	sandbox := &fakeSandbox{available: true, res: SandboxRunResult{ExitCode: 0, Stdout: "ok"}}
	svc.WithSandboxRunner(sandbox)

	if _, err := svc.RunCommandIn("git", []string{"status"}, "mcp-devbox"); err != nil {
		t.Fatal(err)
	}
	if sandbox.gotDir != repo {
		t.Fatalf("sandbox dir = %q, want %q", sandbox.gotDir, repo)
	}
}

func TestRunCommandRequiresPrivateSandboxAndNeverUsesHostRunner(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	hostRan := false
	svc.WithRunner(func(_ context.Context, _ string, _ string, _ []string) (string, error) {
		hostRan = true
		return "host", nil
	})
	if _, err := svc.RunCommand("git", []string{"status"}); err == nil || !strings.Contains(err.Error(), "private L3") {
		t.Fatalf("uncontained command error = %v", err)
	}
	if hostRan {
		t.Fatal("run_command fell back to the host runner")
	}
}

func TestRunCommand_DeniesCWDOutsideWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	outside := filepath.Join(filepath.Dir(root), "outside-cwd")
	write(t, outside, "x.txt", "x")
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "should not run"}})

	if _, err := svc.RunCommandIn("git", []string{"status"}, outside); err == nil {
		t.Fatal("run_command cwd must not escape the workspace jail")
	}
}

func TestRunCommand_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "x"}})
	if _, err := svc.RunCommand("git", []string{"status"}); err == nil {
		t.Error("run_command in read-only should be denied")
	}
}

func TestRunCommandAskModeFailsClosed(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	sandbox := &fakeSandbox{available: true, res: SandboxRunResult{Stdout: "ran"}}
	svc.WithSandboxRunner(sandbox)
	_, err := svc.RunCommand("git", []string{"status"})
	if !errors.Is(err, policy.ErrExecutionRequiresAllow) {
		t.Fatalf("ask mode should require administrator-selected allow mode: %v", err)
	}
	if sandbox.runs != 0 {
		t.Fatal("ask mode executed repository code")
	}
}

func TestRunCommand_NonAllowlistedDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "x"}})
	if _, err := svc.RunCommand("python", []string{"-c", "print(1)"}); !errors.Is(err, policy.ErrCommandNotAllowed) {
		t.Errorf("non-allowlisted command should be denied, got %v", err)
	}
}

func TestRunCommand_InjectionDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "x"}})
	if _, err := svc.RunCommand("git", []string{"status; rm -rf /"}); !errors.Is(err, policy.ErrCommandInjection) {
		t.Errorf("injected metacharacters should be denied, got %v", err)
	}
}

func TestRunCommand_DestructiveDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "x"}})
	// git is allowlisted, but push is blocked as destructive.
	if _, err := svc.RunCommand("git", []string{"push", "--force"}); !errors.Is(err, policy.ErrCommandDestructive) {
		t.Errorf("destructive git should be denied, got %v", err)
	}
}

func TestRunCommand_RedactsOutput(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "leaked ghp_0123456789abcdefghijklmnopqrstuvwxyz here"}})
	out, err := svc.RunCommand("git", []string{"log"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("run_command leaked a secret: %q", out)
	}
}
