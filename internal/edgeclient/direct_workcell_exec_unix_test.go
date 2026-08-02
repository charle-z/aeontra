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
	spec DirectWorkcellProcessSpec
	exit int
	wait bool
}

func (runner *fakeDirectWorkcellRunner) Run(ctx context.Context, spec DirectWorkcellProcessSpec) (int, error) {
	runner.spec = spec
	if runner.wait {
		<-ctx.Done()
		return -1, ctx.Err()
	}
	_, _ = io.WriteString(spec.Stdout, "ok\n")
	_, _ = io.WriteString(spec.Stderr, "warning\n")
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
	runner := &fakeDirectWorkcellRunner{exit: 7}
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
