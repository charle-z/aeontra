//go:build !windows

package main

import (
	"context"
	"errors"
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
	if code := waitBundleUnitCompletion(ctx, "mcp-devbox-edge-repair.service", time.Minute); code != "cancelled" {
		t.Fatalf("cancelled bundle wait returned %q", code)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled bundle wait took %s", elapsed)
	}
}

func TestBundleRecoveryWaitsForActivatingOneshot(t *testing.T) {
	previous := queryBundleUnit
	t.Cleanup(func() { queryBundleUnit = previous })
	queries := 0
	queryBundleUnit = func(_ context.Context, args ...string) (string, error) {
		queries++
		if args[0] == "is-active" {
			return "activating", errors.New("is-active exits nonzero while oneshot activates")
		}
		if queries == 1 {
			return "LoadState=loaded\nActiveState=activating\nResult=success\n", nil
		}
		return "LoadState=loaded\nActiveState=inactive\nResult=success\n", nil
	}
	if code := waitBundleUnitCompletion(context.Background(), "mcp-devbox-bundle-updater.service", 3*time.Second); code != "" {
		t.Fatalf("successful oneshot returned %q", code)
	}
	if queries != 2 {
		t.Fatalf("recovery completed before oneshot finished: queries=%d", queries)
	}
}

func TestBundleRecoveryRequiresSuccessfulAuthoritativeUnitState(t *testing.T) {
	previous := queryBundleUnit
	t.Cleanup(func() { queryBundleUnit = previous })
	for _, test := range []struct {
		name, output, code string
		err                error
	}{
		{"success", "LoadState=loaded\nActiveState=inactive\nResult=success\n", "", nil},
		{"failed", "LoadState=loaded\nActiveState=failed\nResult=exit-code\n", "updater_failed", nil},
		{"inactive-failure", "LoadState=loaded\nActiveState=inactive\nResult=timeout\n", "updater_failed", nil},
		{"missing-unit", "LoadState=not-found\nActiveState=inactive\nResult=success\n", "updater_state_unavailable", nil},
		{"query-failure", "", "updater_state_unavailable", errors.New("bus unavailable")},
		{"missing-result", "LoadState=loaded\nActiveState=inactive\n", "updater_state_unavailable", nil},
		{"duplicate", "LoadState=loaded\nActiveState=inactive\nResult=success\nResult=success\n", "updater_state_unavailable", nil},
		{"unknown-state", "LoadState=loaded\nActiveState=unknown\nResult=success\n", "updater_state_unavailable", nil},
		{"unfinished", "LoadState=loaded\nActiveState=activating\nResult=success\n", "updater_timeout", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			queryBundleUnit = func(_ context.Context, args ...string) (string, error) {
				if !reflect.DeepEqual(args, []string{"show", "--property=LoadState", "--property=ActiveState", "--property=Result", "mcp-devbox-bundle-updater.service"}) {
					t.Fatalf("unexpected unit query: %v", args)
				}
				return test.output, test.err
			}
			if code := waitBundleUnitCompletion(context.Background(), "mcp-devbox-bundle-updater.service", 20*time.Millisecond); code != test.code {
				t.Fatalf("completion code = %q, want %q", code, test.code)
			}
		})
	}
}

func TestInterruptedBundleStartPreservesExclusiveReceipt(t *testing.T) {
	previous := startBundleUnit
	t.Cleanup(func() { startBundleUnit = previous })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	starts := 0
	startBundleUnit = func(_ context.Context, unit string) error {
		starts++
		if unit != "mcp-devbox-bundle-updater.service" {
			t.Fatalf("unexpected unit %q", unit)
		}
		// The root oneshot can survive a restart of the Edge that called it.
		cancel()
		return context.Canceled
	}
	root := t.TempDir()
	operation := edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationBundleUpdate}
	if _, code := executeBundleControl(ctx, root, operation); code != "cancelled" {
		t.Fatalf("interrupted start code = %q", code)
	}
	receipt, err := readBundleReceipt(root)
	if err != nil || receipt.OperationID != operation.ID || receipt.Kind != operation.Kind {
		t.Fatalf("restart lost the updater receipt: %+v, %v", receipt, err)
	}
	other := edge.Operation{ID: "eo_abcdef0123456789abcdef0123456789", Kind: edge.OperationBundleUpdate}
	if _, code := executeBundleControl(context.Background(), root, other); code != "updater_busy" {
		t.Fatalf("interrupted update admitted another operation: %q", code)
	}
	if starts != 1 {
		t.Fatalf("updater dispatched %d times", starts)
	}
}

func TestBundleStartFailureStillFailsAndReleasesReceipt(t *testing.T) {
	previous := startBundleUnit
	t.Cleanup(func() { startBundleUnit = previous })
	startBundleUnit = func(context.Context, string) error { return errors.New("unit failed") }
	root := t.TempDir()
	operation := edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationBundleUpdate}
	if _, code := executeBundleControl(context.Background(), root, operation); code != "updater_failed" {
		t.Fatalf("unit failure code = %q", code)
	}
	if _, err := readBundleReceipt(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("definitive start failure retained its receipt: %v", err)
	}
}

func TestBundleCancelledBeforeStartDoesNotCreateReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	operation := edge.Operation{ID: "eo_0123456789abcdef0123456789abcdef", Kind: edge.OperationBundleUpdate}
	if _, code := executeBundleControl(ctx, root, operation); code != "cancelled" {
		t.Fatalf("cancelled start returned %q", code)
	}
	if _, err := readBundleReceipt(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unstarted operation created a receipt: %v", err)
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
	fail    map[string]error
	calls   []string
}

type projectGitTestExitError int

func (e projectGitTestExitError) Error() string { return "Git exited without a matching ref" }
func (e projectGitTestExitError) ExitCode() int { return int(e) }

func (runner *projectSnapshotRunner) Run(_ context.Context, dir string, args []string, _ edgeclient.GitHubCredential) (string, error) {
	key := strings.Join(args, " ")
	runner.calls = append(runner.calls, dir+"|"+key)
	if err := runner.fail[key]; err != nil {
		return "", err
	}
	return runner.outputs[key], nil
}

func TestCollectProjectSnapshotReportsUnbornCheckout(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_0123456789abcdef0123456789abcdef", Path: "/work/project",
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
	runner := &projectSnapshotRunner{outputs: map[string]string{
		"branch --show-current":                          "main\n",
		"symbolic-ref --quiet --short HEAD":              "main\n",
		"status --porcelain=v1 --untracked-files=normal": "?? README.md\n",
	}, fail: map[string]error{
		"rev-parse --verify HEAD":                   errors.New("exit status 128"),
		"show-ref --verify --quiet refs/heads/main": projectGitTestExitError(1),
	}}
	result, code := collectProjectSnapshot(context.Background(), resolved, runner, edgeclient.GitHubCredential{})
	if code != "" || result.SnapshotBranch != "main" || result.SnapshotHead != "" || !result.SnapshotUnborn || result.SnapshotClean || result.ProjectState != "dirty" {
		t.Fatalf("unborn snapshot branch=%q head=%q unborn=%t clean=%t state=%q code=%q", result.SnapshotBranch, result.SnapshotHead, result.SnapshotUnborn, result.SnapshotClean, result.ProjectState, code)
	}
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
