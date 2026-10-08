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

func fakeRunner(output string, err error) Runner {
	return func(ctx context.Context, dir, prog string, args []string) (string, error) {
		return output, err
	}
}

func TestRunTests_AllowMode(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithTestCommand([]string{"go", "test", "./..."}).WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "ok\nPASS\n"}})
	out, err := svc.RunTests()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("unexpected: %q", out)
	}
}

func TestRunTests_RunsInSelectedCWDUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "mcp-devbox")
	write(t, repo, "go.mod", "module example.com/x\n")
	sandbox := &fakeSandbox{available: true, res: SandboxRunResult{Stdout: "PASS\n"}}
	svc.WithTestCommand([]string{"go", "test"}).
		WithSandboxRunner(sandbox)

	if _, err := svc.RunTestsIn("mcp-devbox"); err != nil {
		t.Fatal(err)
	}
	if sandbox.gotDir != repo {
		t.Fatalf("sandbox dir = %q, want %q", sandbox.gotDir, repo)
	}
}

func TestRunTestsRequiresPrivateSandboxAndNeverUsesHostRunner(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	hostRan := false
	svc.WithTestCommand([]string{"go", "test"}).WithRunner(func(_ context.Context, _ string, _ string, _ []string) (string, error) {
		hostRan = true
		return "host", nil
	})
	if _, err := svc.RunTests(); err == nil || !strings.Contains(err.Error(), "private L3") {
		t.Fatalf("uncontained tests error = %v", err)
	}
	if hostRan {
		t.Fatal("run_tests fell back to the host runner")
	}
}

func TestRunTests_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithTestCommand([]string{"go", "test"}).WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "ok"}})
	if _, err := svc.RunTests(); err == nil {
		t.Error("run_tests in read-only mode should be denied")
	}
}

func TestRunTestsAskModeFailsClosed(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithTestCommand([]string{"go", "test"}).WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "ok"}})
	_, err := svc.RunTests()
	if !errors.Is(err, policy.ErrExecutionRequiresAllow) {
		t.Fatalf("ask mode should require administrator-selected allow mode: %v", err)
	}
}

func TestRunTests_NotConfigured(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	if _, err := svc.RunTests(); err == nil {
		t.Error("run_tests without a configured command should error")
	}
}

func TestRunTests_NonAllowlistedBaseCommand(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	// "make" is not in the test allowlist {git, go}.
	svc.WithTestCommand([]string{"make", "test"}).WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: "ok"}})
	if _, err := svc.RunTests(); err == nil {
		t.Error("non-allowlisted test command should be denied")
	}
}

func TestRunTests_RedactsOutput(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	leaky := "running...\nleaked gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz\nPASS"
	svc.WithTestCommand([]string{"go", "test"}).WithSandboxRunner(&fakeSandbox{available: true, res: SandboxRunResult{Stdout: leaky}})
	out, err := svc.RunTests()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("run_tests leaked a secret in output: %q", out)
	}
}
