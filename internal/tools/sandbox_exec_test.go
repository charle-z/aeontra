package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

// fakeSandbox is a test SandboxRunner: it reports a configurable availability and
// records the argv it was asked to run.
type fakeSandbox struct {
	available bool
	res       SandboxRunResult
	err       error
	gotArgv   []string
	gotDir    string
	runs      int
}

func (f *fakeSandbox) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{Available: f.available, Backend: "fake", DefaultEgress: "deny"}
}

func (f *fakeSandbox) Run(_ context.Context, req SandboxRunRequest) (SandboxRunResult, error) {
	f.gotArgv = append([]string(nil), req.Argv...)
	f.gotDir = req.Dir
	f.runs++
	return f.res, f.err
}

func TestSandboxExec_RequiresAvailableBackend(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow) // default sandbox is disabled
	if _, err := svc.SandboxExec([]string{"sh", "-c", "echo hi"}); err == nil {
		t.Fatal("sandbox_exec must be denied when no sandbox backend is available")
	}
	// Even an explicitly-unavailable backend is refused.
	svc.WithSandboxRunner(&fakeSandbox{available: false})
	if _, err := svc.SandboxExec([]string{"ls"}); err == nil {
		t.Fatal("sandbox_exec must be denied when backend reports unavailable")
	}
}

func TestSandboxExec_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithSandboxRunner(&fakeSandbox{available: true})
	if _, err := svc.SandboxExec([]string{"ls"}); err == nil {
		t.Error("sandbox_exec in read-only mode must be denied")
	}
}

func TestSandboxExecAskModeFailsClosed(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	fs := &fakeSandbox{available: true, res: SandboxRunResult{ExitCode: 0, Stdout: "ran"}}
	svc.WithSandboxRunner(fs)
	_, err := svc.SandboxExec([]string{"whoami"})
	if !errors.Is(err, policy.ErrExecutionRequiresAllow) {
		t.Fatalf("ask mode should require administrator-selected allow mode: %v", err)
	}
	if fs.gotArgv != nil {
		t.Error("ask mode executed repository code")
	}
}

func TestSandboxExec_AllowRunsArbitraryCommandAndRedacts(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow)
	fs := &fakeSandbox{
		available: true,
		res: SandboxRunResult{
			ExitCode:       0,
			Stdout:         "leaked ghp_0123456789abcdefghijklmnopqrstuvwxyz here",
			SandboxBackend: "fake",
			EgressProfile:  "none",
		},
	}
	svc.WithSandboxRunner(fs)
	// Arbitrary command incl. a shell — allowed because the sandbox contains it.
	out, err := svc.SandboxExec([]string{"sh", "-c", "cat /etc/os-release"})
	if err != nil {
		t.Fatal(err)
	}
	if fs.gotArgv == nil || fs.gotArgv[0] != "sh" {
		t.Errorf("argv should reach the sandbox verbatim, got %v", fs.gotArgv)
	}
	if strings.Contains(out, "ghp_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("sandbox output must be redacted: %q", out)
	}
	if !strings.Contains(out, "exit 0") {
		t.Errorf("result should report exit status: %q", out)
	}
}
