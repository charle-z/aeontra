//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type windowsDirectWorkcellFakeRunner struct {
	spec  DirectWorkcellProcessSpec
	exit  int
	err   error
	wait  bool
	write func(io.Writer, io.Writer)
}

func (runner *windowsDirectWorkcellFakeRunner) Run(ctx context.Context, spec DirectWorkcellProcessSpec) (int, error) {
	runner.spec = spec
	if runner.write != nil {
		runner.write(spec.Stdout, spec.Stderr)
	}
	if runner.wait {
		<-ctx.Done()
		return -1, ctx.Err()
	}
	return runner.exit, runner.err
}

func windowsDirectWorkcellFixture(t *testing.T) Workspace {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	handle, err := openWindowsSecurityHandle(root, true, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyCurrentIdentityPrivateACL(handle, true); err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatal(err)
	}
	_ = windows.CloseHandle(handle)
	path := filepath.Join(root, "workspace")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: path, WindowsDevRoot: root, Profile: WorkspaceProfileWindowsWorkcell, Mode: WorkspaceModeDev, NetworkPosture: WindowsWorkcellNetworkPosture}
}

func windowsDirectWorkcellRequest(workspace Workspace) DirectWorkcellCommandRequest {
	return DirectWorkcellCommandRequest{
		OperationID:    "eo_0123456789abcdef0123456789abcdef",
		Workspace:      workspace,
		WindowsDevRoot: workspace.WindowsDevRoot,
		Argv:           []string{"cmd.exe", "/c", "echo", "ok"},
		Environment:    map[string]string{"CI": "true"},
		TimeoutSeconds: 5,
	}
}

func TestWindowsDirectWorkcellRejectsWorkspaceOutsideRegisteredRoot(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.WindowsDevRoot = t.TempDir()
	_, err := RunDirectWorkcellCommand(context.Background(), request, &windowsDirectWorkcellFakeRunner{})
	if !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("err=%v, want registered-root contract error", err)
	}
}

func TestWindowsDirectWorkcellRequiresWindowsDevProfile(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.Workspace.Profile = WorkspaceProfileLinuxWorkcell
	_, err := RunDirectWorkcellCommand(context.Background(), request, &windowsDirectWorkcellFakeRunner{})
	if !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("err=%v, want direct workcell contract error", err)
	}
}

func TestWindowsDirectWorkcellKeepsCodexHomeInsideWorkspace(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.Environment["CODEX_HOME"] = filepath.Join(t.TempDir(), "codex-home")
	if _, err := RunDirectWorkcellCommand(context.Background(), request, &windowsDirectWorkcellFakeRunner{}); !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("outside CODEX_HOME err=%v, want contract rejection", err)
	}
	request.Environment["CODEX_HOME"] = filepath.Join(workspace.Path, ".codex-test")
	runner := &windowsDirectWorkcellFakeRunner{}
	if _, err := RunDirectWorkcellCommand(context.Background(), request, runner); err != nil {
		t.Fatal(err)
	}
	if runner.spec.codeHomeHandle == nil {
		t.Fatal("workspace-scoped CODEX_HOME did not retain a validated handle")
	}
}

func TestWindowsDirectWorkcellBuildsExplicitArgvPrivateTempAndSanitizedEnvironment(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	runner := &windowsDirectWorkcellFakeRunner{exit: 7}
	result, err := RunDirectWorkcellCommand(context.Background(), windowsDirectWorkcellRequest(workspace), runner)
	if err != nil || !result.Completed || result.ExitCode != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.EqualFold(filepath.Base(runner.spec.Executable), "cmd.exe") || runner.spec.Args[0] != "/c" {
		t.Fatalf("spec executable/args=%q %q, want explicit cmd argv", runner.spec.Executable, runner.spec.Args)
	}
	if runner.spec.Dir == "" || !strings.HasPrefix(strings.ToLower(filepath.Clean(runner.spec.Dir)), strings.ToLower(filepath.Clean(workspace.Path))) {
		t.Fatalf("spec dir=%q, want workspace descendant", runner.spec.Dir)
	}
	if runner.spec.workspaceHandle == nil || runner.spec.cwdHandle == nil || runner.spec.tempHandle == nil {
		t.Fatal("spec did not retain workspace, cwd, and temp handles")
	}
	environment := strings.Join(runner.spec.Env, "\x00")
	for _, forbidden := range []string{"HOME=", "USERPROFILE=", "CONTAINER_HOST=", "TOKEN=", "SECRET="} {
		if strings.Contains(strings.ToUpper(environment), forbidden) {
			t.Fatalf("environment contains forbidden material %q: %q", forbidden, environment)
		}
	}
	for _, required := range []string{"SystemRoot=", "ComSpec=", "PATHEXT=", "PATH=", "TEMP=", "TMP=", "MCP_DEVBOX_PROFILE=windows-workcell", "MCP_DEVBOX_MODE=dev"} {
		if !strings.Contains(environment, required) {
			t.Fatalf("environment missing %q: %q", required, environment)
		}
	}
	temp := environment[strings.Index(environment, "TEMP=")+len("TEMP="):]
	if end := strings.IndexByte(temp, 0); end >= 0 {
		temp = temp[:end]
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Clean(temp)), strings.ToLower(filepath.Clean(workspace.Path))) {
		t.Fatalf("temp=%q is outside workspace=%q", temp, workspace.Path)
	}
	if len(runner.spec.Args) != 3 || runner.spec.Args[0] != "/c" || runner.spec.Args[1] != "echo" || runner.spec.Args[2] != "ok" {
		t.Fatalf("args=%q, want no implicit shell wrapping", runner.spec.Args)
	}
}

func TestWindowsDirectWorkcellRedactsAndBoundsOutput(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	runner := &windowsDirectWorkcellFakeRunner{exit: 0, write: func(stdout, stderr io.Writer) {
		_, _ = io.WriteString(stdout, "api_key=supersecretvalue123\n"+strings.Repeat("x", 2*int(24<<10)))
		_, _ = io.WriteString(stderr, "password=anothersecret123\n"+strings.Repeat("y", 2*int(24<<10)))
	}}
	result, err := RunDirectWorkcellCommand(context.Background(), windowsDirectWorkcellRequest(workspace), runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stdout) > 24<<10 || len(result.Stderr) > 24<<10 || !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("result bounds=%+v", result)
	}
	for _, leaked := range []string{"supersecretvalue123", "anothersecret123"} {
		if strings.Contains(result.Stdout, leaked) || strings.Contains(result.Stderr, leaked) {
			t.Fatalf("secret leaked in result: %q", leaked)
		}
	}
}

func TestWindowsDirectWorkcellTimeoutUsesJobRunnerContext(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.TimeoutSeconds = 1
	runner := &windowsDirectWorkcellFakeRunner{wait: true, write: func(stdout, stderr io.Writer) {
		_, _ = io.WriteString(stdout, "still bounded\n")
	}}
	started := time.Now()
	result, err := RunDirectWorkcellCommand(context.Background(), request, runner)
	if err != nil || !result.TimedOut || result.ExitCode != -1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("timeout result took too long: %s", time.Since(started))
	}
}

func TestWindowsDirectWorkcellExecutesNativeCmdWithoutProfileInheritance(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.Argv = []string{"cmd.exe", "/D", "/C", "if defined USERPROFILE (exit /b 9) else (ver)"}
	result, err := RunDirectWorkcellCommand(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || !strings.Contains(strings.ToLower(result.Stdout), "windows") {
		t.Fatalf("native cmd result=%+v", result)
	}
}

func TestWindowsDirectWorkcellExecutesNativePowerShell(t *testing.T) {
	workspace := windowsDirectWorkcellFixture(t)
	request := windowsDirectWorkcellRequest(workspace)
	request.Argv = []string{"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.ToString()"}
	result, err := RunDirectWorkcellCommand(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("native PowerShell result=%+v", result)
	}
}
