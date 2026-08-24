//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows"
)

type windowsProjectExecRunner struct {
	exit int
}

func (runner windowsProjectExecRunner) Run(ctx context.Context, spec edgeclient.DirectWorkcellProcessSpec) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	_, _ = io.WriteString(spec.Stdout, "ok\n")
	_, _ = io.WriteString(spec.Stderr, "warning\n")
	return runner.exit, nil
}

func windowsProjectExecResolution(t *testing.T) edgeclient.ProjectResolution {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	secureWindowsProjectExecFixtureRoot(t, root)
	path := filepath.Join(root, "project")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"},
		TargetAlias: "windows",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: path,
			WindowsDevRoot: root,
			Profile:        edgeclient.WorkspaceProfileWindowsWorkcell, Mode: edgeclient.WorkspaceModeDev,
			NetworkPosture: edgeclient.WindowsWorkcellNetworkPosture,
		},
	}
}

func secureWindowsProjectExecFixtureRoot(t *testing.T, root string) {
	t.Helper()
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		t.Fatal(err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		t.Fatalf("test token unavailable: %v", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("test ACL unavailable: %v", err)
	}
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCollectWindowsProjectExecMapsResultAndTiming(t *testing.T) {
	resolved := windowsProjectExecResolution(t)
	operation := edge.Operation{
		ID:      "eo_0123456789abcdef0123456789abcdef",
		Request: edge.OperationRequest{Argv: []string{"cmd.exe", "/c", "echo", "ok"}, TimeoutSeconds: 10},
	}
	result, code := collectProjectExec(context.Background(), operation, resolved, windowsProjectExecRunner{exit: 7})
	if code != "" || !result.ExecCompleted || result.ExecExitCode != 7 || result.ExecStdout != "ok\n" || result.ExecStderr != "warning\n" {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	if !result.ExecTimingKnown || result.ExecPreflightUS < 0 || result.ExecExecutionUS < 0 || result.ExecResultUS < 0 {
		t.Fatalf("timing=%+v", result)
	}
	if result.WorkspaceID != resolved.Workspace.ID || result.ProjectAlias != resolved.Project.Alias || result.ProjectProfile != "windows-workcell" || result.ProjectMode != "dev" {
		t.Fatalf("metadata=%+v", result)
	}
}

func TestCollectWindowsProjectExecMapsContractAndCancellationCodes(t *testing.T) {
	resolved := windowsProjectExecResolution(t)
	operation := edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef", Request: edge.OperationRequest{Argv: []string{"cmd.exe"}, TimeoutSeconds: 10}}
	wrong := resolved
	wrong.Workspace.Mode = edgeclient.WorkspaceModeHTBLinux
	if result, code := collectProjectExec(context.Background(), operation, wrong, windowsProjectExecRunner{}); code != "project_exec_invalid" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("invalid result=%+v code=%q", result, code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, code := collectProjectExec(ctx, operation, resolved, windowsProjectExecRunner{}); code != "cancelled" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("cancelled result=%+v code=%q", result, code)
	}
}
