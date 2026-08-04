//go:build !windows

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
)

type projectExecRunner struct {
	spec edgeclient.DirectWorkcellProcessSpec
	exit int
}

func (runner *projectExecRunner) Run(_ context.Context, spec edgeclient.DirectWorkcellProcessSpec) (int, error) {
	runner.spec = spec
	_, _ = io.WriteString(spec.Stdout, "ok\n")
	_, _ = io.WriteString(spec.Stderr, "warning\n")
	return runner.exit, nil
}

func TestCollectProjectExecMapsBoundedWorkcellResult(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath,
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
	operation := edge.Operation{
		ID: "eo_0123456789abcdef0123456789abcdef",
		Request: edge.OperationRequest{
			Argv: []string{"go", "test", "./..."}, Environment: map[string]string{"CI": "true"}, TimeoutSeconds: 30,
		},
	}
	runner := &projectExecRunner{exit: 7}
	result, code := collectProjectExec(context.Background(), operation, resolved, runner)
	if code != "" || !result.ExecCompleted || result.ExecExitCode != 7 || result.ExecStdout != "ok\n" || result.ExecStderr != "warning\n" {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	if !result.ExecTimingKnown || result.ExecPreflightUS < 0 || result.ExecExecutionUS < 0 || result.ExecResultUS < 0 {
		t.Fatalf("execution timing=%+v", result)
	}
	if result.WorkspaceID != resolved.Workspace.ID || result.ProjectAlias != "mcp-devbox" || result.ProjectOwner != "charle-z" ||
		result.ProjectRepository != "mcp-devbox" || result.ProjectTarget != "parrot" || result.ProjectState != "ready" ||
		result.ProjectProfile != "linux-workcell" || result.ProjectMode != "dev" {
		t.Fatalf("metadata=%+v", result)
	}
	if runner.spec.Executable != "bwrap" {
		t.Fatalf("runner spec=%+v", runner.spec)
	}
}

func TestCollectProjectExecRejectsWrongWorkspaceMode(t *testing.T) {
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath,
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeHTBLinux,
		},
	}
	operation := edge.Operation{
		ID:      "eo_0123456789abcdef0123456789abcdef",
		Request: edge.OperationRequest{Argv: []string{"pwd"}, TimeoutSeconds: 10},
	}
	result, code := collectProjectExec(context.Background(), operation, resolved, &projectExecRunner{})
	if code != "project_exec_invalid" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("result=%+v code=%q", result, code)
	}
}
