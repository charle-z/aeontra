package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

// fakeSandbox is a test SandboxRunner: it reports a configurable availability and
// records the argv it was asked to run.
type fakeSandbox struct {
	available bool
	res       SandboxRunResult
	err       error
	gotArgv   []string
}

func (f *fakeSandbox) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{Available: f.available, Backend: "fake", DefaultEgress: "deny"}
}

func (f *fakeSandbox) Run(_ context.Context, req SandboxRunRequest) (SandboxRunResult, error) {
	f.gotArgv = req.Argv
	return f.res, f.err
}

func TestSandboxExec_RequiresAvailableBackend(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAllow) // default sandbox is disabled
	if _, err := svc.SandboxExec([]string{"sh", "-c", "echo hi"}, true); err == nil {
		t.Fatal("sandbox_exec must be denied when no sandbox backend is available")
	}
	// Even an explicitly-unavailable backend is refused.
	svc.WithSandboxRunner(&fakeSandbox{available: false})
	if _, err := svc.SandboxExec([]string{"ls"}, true); err == nil {
		t.Fatal("sandbox_exec must be denied when backend reports unavailable")
	}
}

func TestSandboxExec_ReadOnlyDenied(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	svc.WithSandboxRunner(&fakeSandbox{available: true})
	if _, err := svc.SandboxExec([]string{"ls"}, true); err == nil {
		t.Error("sandbox_exec in read-only mode must be denied")
	}
}

func TestSandboxExec_AskRequiresApproval(t *testing.T) {
	svc, _ := newTestService(t, config.ModeAsk)
	fs := &fakeSandbox{available: true, res: SandboxRunResult{ExitCode: 0, Stdout: "ran"}}
	svc.WithSandboxRunner(fs)
	msg, err := svc.SandboxExec([]string{"whoami"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "APPROVAL REQUIRED") {
		t.Errorf("ask mode should require approval: %q", msg)
	}
	if fs.gotArgv != nil {
		t.Error("nothing should run before approval")
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
	out, err := svc.SandboxExec([]string{"sh", "-c", "cat /etc/os-release"}, false)
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

// End-to-end: sandbox_exec runs an arbitrary command in a REAL Docker sandbox,
// contained and as a non-root user. Linux + Docker only (skips elsewhere).
func TestSandboxExec_Integration_RealDocker(t *testing.T) {
	requireDockerSandbox(t)
	svc, root := newTestService(t, config.ModeAllow)
	svc.WithSandboxRunner(NewDockerSandboxRunner(DockerSandboxConfig{
		Image:   "alpine:3.22",
		Root:    root,
		Timeout: 60 * time.Second,
	}))
	// An arbitrary shell command runs inside the sandbox.
	out, err := svc.SandboxExec([]string{"sh", "-c", "echo SANDBOXOK; id -u"}, false)
	if err != nil {
		t.Fatalf("sandbox_exec e2e: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SANDBOXOK") || !strings.Contains(out, "exit 0") {
		t.Fatalf("expected successful contained execution, got: %q", out)
	}
	if !strings.Contains(out, "10001") {
		t.Errorf("command should run as the non-root sandbox user (uid 10001): %q", out)
	}
}
