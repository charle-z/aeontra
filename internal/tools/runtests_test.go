package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func fakeRunner(output string, err error) Runner {
	return func(ctx context.Context, dir, prog string, args []string) (string, error) {
		return output, err
	}
}

func TestRunTests_AllowMode(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	svc.WithTestCommand([]string{"go", "test", "./..."}).WithRunner(fakeRunner("ok\nPASS\n", nil))
	out, err := svc.RunTests(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("unexpected: %q", out)
	}
}

func TestRunTests_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithTestCommand([]string{"go", "test"}).WithRunner(fakeRunner("ok", nil))
	if _, err := svc.RunTests(true); err == nil {
		t.Error("run_tests in read-only mode should be denied")
	}
}

func TestRunTests_AskRequiresApproval(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	svc.WithTestCommand([]string{"go", "test"}).WithRunner(fakeRunner("ok", nil))
	msg, err := svc.RunTests(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") {
		t.Errorf("ask mode should require approval: %q", msg)
	}
}

func TestRunTests_NotConfigured(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	if _, err := svc.RunTests(true); err == nil {
		t.Error("run_tests without a configured command should error")
	}
}

func TestRunTests_NonAllowlistedBaseCommand(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	// "make" is not in the test allowlist {git, go}.
	svc.WithTestCommand([]string{"make", "test"}).WithRunner(fakeRunner("ok", nil))
	if _, err := svc.RunTests(true); err == nil {
		t.Error("non-allowlisted test command should be denied")
	}
}

func TestRunTests_RedactsOutput(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	leaky := "running...\nleaked gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz\nPASS"
	svc.WithTestCommand([]string{"go", "test"}).WithRunner(fakeRunner(leaky, nil))
	out, err := svc.RunTests(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("run_tests leaked a secret in output: %q", out)
	}
}
