//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestBundleUnitWaitHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if waitBundleUnitInactive(ctx, "mcp-devbox-edge-repair.service", time.Minute) {
		t.Fatal("cancelled bundle wait reported inactive")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled bundle wait took %s", elapsed)
	}
}

func TestBundleOperationReceiptIsDurableExclusiveAndValidated(t *testing.T) {
	stateRoot := t.TempDir()
	receipt := bundleOperationReceipt{OperationID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationBundleRollback}
	if err := writeBundleReceipt(stateRoot, receipt); err != nil {
		t.Fatal(err)
	}
	read, err := readBundleReceipt(stateRoot)
	if err != nil || read != receipt {
		t.Fatalf("read receipt = %+v, %v", read, err)
	}
	other := bundleOperationReceipt{OperationID: "eo_abcdef0123456789abcdef0123456789", Kind: edge.OperationBundleUpdate}
	if err := writeBundleReceipt(stateRoot, other); err == nil {
		t.Fatal("expected an existing receipt to prevent a second updater operation")
	}
	read, err = readBundleReceipt(stateRoot)
	if err != nil || read != receipt {
		t.Fatalf("existing receipt changed = %+v, %v", read, err)
	}
	clearBundleReceipt(stateRoot, other.OperationID)
	if _, err := readBundleReceipt(stateRoot); err != nil {
		t.Fatalf("unrelated completion cleared receipt: %v", err)
	}
	clearBundleReceipt(stateRoot, receipt.OperationID)
	if _, err := readBundleReceipt(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("receipt was not cleared: %v", err)
	}
}

func TestBundleOperationReceiptFailsClosedOnUnsafeState(t *testing.T) {
	stateRoot := t.TempDir()
	path := filepath.Join(stateRoot, bundleReceiptFile)
	if err := os.WriteFile(path, []byte("{\"operation_id\":\"bad\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundleReceipt(stateRoot); err == nil {
		t.Fatal("expected malformed receipt rejection")
	}
	if err := writeBundleReceipt(stateRoot, bundleOperationReceipt{OperationID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationEdgeRepair}); err == nil {
		t.Fatal("expected malformed existing receipt to block overwrite")
	}
}

func TestInstalledModelProviderAcceptsOnlyClosedLoopbackConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.json")
	valid := []byte("{\"version\":1,\"provider\":\"opencode-local\",\"endpoint\":\"http://127.0.0.1:4096/v1/next-action\"}\n")
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if !installedModelProviderValid(path) {
		t.Fatal("expected closed loopback model configuration to be valid")
	}
	for _, invalid := range []string{
		`{"version":1,"provider":"opencode-local","endpoint":"https://127.0.0.1:4096/v1/next-action"}`,
		`{"version":1,"provider":"opencode-local","endpoint":"http://example.com:4096/v1/next-action"}`,
		`{"version":1,"provider":"opencode-local","endpoint":"http://127.0.0.1:4096/other"}`,
		`{"version":1,"provider":"remote","endpoint":"http://127.0.0.1:4096/v1/next-action"}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if installedModelProviderValid(path) {
			t.Fatalf("accepted unsafe model config: %s", invalid)
		}
	}
}

type projectSnapshotRunner struct {
	outputs map[string]string
	calls   []string
}

func (runner *projectSnapshotRunner) Run(_ context.Context, dir string, args []string, _ edgeclient.GitHubCredential) (string, error) {
	key := strings.Join(args, " ")
	runner.calls = append(runner.calls, dir+"|"+key)
	return runner.outputs[key], nil
}

func TestCollectProjectSnapshotUsesOnlyFixedReadOnlyGitCommands(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: "/home/charles/workspaces/mcp-devbox",
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
	runner := &projectSnapshotRunner{outputs: map[string]string{
		"rev-parse --verify HEAD":                        "0123456789abcdef0123456789abcdef01234567\n",
		"branch --show-current":                          "main\n",
		"status --porcelain=v1 --untracked-files=normal": "?? .mcp-devbox/runtime/\n",
	}}
	result, code := collectProjectSnapshot(context.Background(), resolved, runner, edgeclient.GitHubCredential{})
	if code != "" || result.SnapshotHead != "0123456789abcdef0123456789abcdef01234567" ||
		result.SnapshotBranch != "main" || !result.SnapshotClean || result.WorkspaceID != resolved.Workspace.ID {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	expected := []string{
		resolved.Workspace.Path + "|rev-parse --verify HEAD",
		resolved.Workspace.Path + "|branch --show-current",
		resolved.Workspace.Path + "|status --porcelain=v1 --untracked-files=normal",
	}
	if strings.Join(runner.calls, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("calls=%v", runner.calls)
	}
}

func TestCollectProjectSnapshotReportsDirtyAndFailsClosedForWrongWorkspace(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: "/home/charles/workspaces/repo",
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
	runner := &projectSnapshotRunner{outputs: map[string]string{
		"rev-parse --verify HEAD":                        "0123456789abcdef0123456789abcdef01234567",
		"branch --show-current":                          "main",
		"status --porcelain=v1 --untracked-files=normal": " M changed.go\n",
	}}
	result, code := collectProjectSnapshot(context.Background(), resolved, runner, edgeclient.GitHubCredential{})
	if code != "" || result.SnapshotClean || result.ProjectState != "dirty" || result.SnapshotBranch != "main" || result.SnapshotHead == "" {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	resolved.Workspace.Mode = edgeclient.WorkspaceModeHTBLinux
	result, code = collectProjectSnapshot(context.Background(), resolved, runner, edgeclient.GitHubCredential{})
	if code != "project_snapshot_invalid" || !reflect.DeepEqual(result, edge.OperationResult{}) {
		t.Fatalf("wrong-mode result=%+v code=%q", result, code)
	}
}
