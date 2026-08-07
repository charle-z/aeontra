//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeDirectWorkcellRunner struct {
	spec         DirectWorkcellProcessSpec
	exit         int
	wait         bool
	customOutput bool
	stdout       string
	stderr       string
	delay        time.Duration
}

func (runner *fakeDirectWorkcellRunner) Run(ctx context.Context, spec DirectWorkcellProcessSpec) (int, error) {
	runner.spec = spec
	if runner.delay > 0 {
		time.Sleep(runner.delay)
	}
	if runner.wait {
		<-ctx.Done()
		return -1, ctx.Err()
	}
	stdout := "ok\n"
	stderr := "warning\n"
	if runner.customOutput {
		stdout = runner.stdout
		stderr = runner.stderr
	}
	_, _ = io.WriteString(spec.Stdout, stdout)
	_, _ = io.WriteString(spec.Stderr, stderr)
	return runner.exit, nil
}

func TestRunDirectWorkcellCommandUsesTrustedWorkspaceSandbox(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspacePath, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &fakeDirectWorkcellRunner{exit: 7, delay: 5 * time.Millisecond}
	result, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef", Workspace: workspace,
		Argv: []string{"go", "test", "./..."}, CWD: "internal", Stdin: "input\n",
		Environment: map[string]string{"CI": "true"}, TimeoutSeconds: 30,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.ExitCode != 7 || result.Stdout != "ok\n" || result.Stderr != "warning\n" || result.TimedOut {
		t.Fatalf("result=%+v", result)
	}
	if !result.TimingKnown || result.PreflightUS < 0 || result.ExecutionUS < 5000 || result.ResultUS < 0 {
		t.Fatalf("timing=%+v", result)
	}
	args := runner.spec.Args
	for _, required := range []string{"--die-with-parent", "--new-session", "--unshare-all", "--share-net", "--clearenv", "--bind", workspacePath, "/workspace", "--chdir", "/workspace/internal", "--setenv", "CI", "true", "--", "go", "test", "./..."} {
		if !slices.Contains(args, required) {
			t.Fatalf("sandbox args missing %q: %v", required, args)
		}
	}
	if runner.spec.Dir != workspacePath || runner.spec.Stdin == nil || runner.spec.Stdout == nil || runner.spec.Stderr == nil {
		t.Fatalf("spec=%+v", runner.spec)
	}
	stdin, err := io.ReadAll(runner.spec.Stdin)
	if err != nil || string(stdin) != "input\n" {
		t.Fatalf("stdin=%q err=%v", stdin, err)
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--setenv\nTMPDIR\n/tmp") || strings.Contains(joined, "--setenv\nTMPDIR\n/workspace/.mcp-devbox/runtime/tmp") {
		t.Fatalf("workcell did not use private short tmpfs: %v", args)
	}
	if info, err := os.Stat("/etc/alternatives"); err == nil && info.IsDir() && !strings.Contains(joined, "--ro-bind\n/etc/alternatives\n/etc/alternatives") {
		t.Fatalf("workcell did not expose system alternatives read-only: %v", args)
	}
	if info, err := os.Stat("/etc/chromium.d"); err == nil && info.IsDir() && !strings.Contains(joined, "--ro-bind\n/etc/chromium.d\n/etc/chromium.d") {
		t.Fatalf("workcell did not expose Chromium system configuration read-only: %v", args)
	}
	if _, err := os.Stat(filepath.Join(workspacePath, ".mcp-devbox", "runtime", "tmp")); !os.IsNotExist(err) {
		t.Fatalf("workspace runtime tmp should not be created: %v", err)
	}
	for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "/mnt/c", "/mnt/d", "/root"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sandbox exposed forbidden path %s: %v", forbidden, args)
		}
	}
}

func TestRunDirectWorkcellCommandReturnsBoundedTimeoutResult(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &fakeDirectWorkcellRunner{wait: true}
	started := time.Now()
	result, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef", Workspace: workspace,
		Argv: []string{"sleep", "10"}, TimeoutSeconds: 1,
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || !result.TimedOut || result.ExitCode != -1 || time.Since(started) > 3*time.Second {
		t.Fatalf("result=%+v elapsed=%s", result, time.Since(started))
	}
}

func TestRunDirectWorkcellCommandRejectsEscapingCWD(t *testing.T) {
	root := t.TempDir()
	workspacePath := filepath.Join(root, "project")
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspacePath, "escape")); err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	_, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef", Workspace: workspace,
		Argv: []string{"pwd"}, CWD: "escape", TimeoutSeconds: 10,
	}, &fakeDirectWorkcellRunner{})
	if err == nil || !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunDirectWorkcellCommandRejectsContainerHelperAuthorityEnvironment(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	for _, key := range []string{"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES", "CONTAINERS_STORAGE_CONF"} {
		runner := &fakeDirectWorkcellRunner{}
		_, err := RunDirectWorkcellCommand(context.Background(), DirectWorkcellCommandRequest{
			OperationID: "eo_0123456789abcdef0123456789abcdef", Workspace: workspace,
			Argv: []string{"true"}, Environment: map[string]string{key: "/workspace/untrusted"}, TimeoutSeconds: 10,
		}, runner)
		if !errors.Is(err, ErrDirectWorkcellContract) {
			t.Fatalf("reserved environment %s err=%v", key, err)
		}
		if len(runner.spec.Args) != 0 {
			t.Fatalf("reserved environment %s reached runner: %v", key, runner.spec.Args)
		}
	}
}
