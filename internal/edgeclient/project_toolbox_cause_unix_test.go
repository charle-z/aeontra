//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectToolboxInspectionPreservesCause(t *testing.T) {
	for index, pattern := range []string{"{{index .Config.Labels", ".State.Status", "--size --format", "json .Mounts", "HostConfig.Memory", "json .Config.Env"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("private cause must not escape")} {
			t.Run(fmt.Sprintf("stage%d/%s", index, cause), func(t *testing.T) {
				root := t.TempDir()
				workspace := Workspace{ID: "ws_55555555555555555555555555555555", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
				runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(root, "podman.sock")}
				manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: root, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"}, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
					t.Fatal(err)
				}
				runner.calls = nil
				runner.fail, runner.failError = pattern, cause
				runner.psLabelOutput = strings.Repeat("c", 64)
				_, err = manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
				if !errors.Is(err, cause) {
					t.Fatalf("original cause lost: %v", err)
				}
				if strings.Contains(err.Error(), "private") {
					t.Fatalf("diagnostic leaked: %v", err)
				}
				if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
					for _, call := range runner.calls {
						if strings.Contains(strings.Join(call, " "), "ps -aq") {
							t.Fatal("cancellation triggered additional engine probes")
						}
					}
				}
			})
		}
	}
}

func TestProjectToolboxFailureDiagnosticDoesNotExposeContent(t *testing.T) {
	for _, tc := range []struct {
		cause    error
		category string
		exit     int
	}{
		{nil, "invalid_response", -1},
		{context.Canceled, "cancelled", -1},
		{context.DeadlineExceeded, "timeout", -1},
		{&os.PathError{Op: "exec", Path: "/private/token", Err: os.ErrPermission}, "permission_denied", -1},
		{&os.PathError{Op: "exec", Path: "/private/token", Err: os.ErrNotExist}, "process_start_failed", -1},
		{errors.New("token=secret /private/path"), "unknown", -1},
	} {
		err := toolboxInspectionError(ErrProjectToolboxOwnershipInspectUnavailable, ErrProjectToolboxUnavailable, tc.cause)
		stage, category, code, ok := ProjectToolboxFailureDiagnostic(err)
		if !ok || stage != "ownership" || category != tc.category || code != tc.exit {
			t.Fatalf("diagnostic=%s/%s/%d/%t", stage, category, code, ok)
		}
		if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
			t.Fatal("sensitive diagnostic leaked")
		}
	}
	err := exec.Command("/bin/sh", "-c", "exit 125").Run()
	_, category, code, ok := ProjectToolboxFailureDiagnostic(toolboxInspectionError(ErrProjectToolboxStateInspectUnavailable, ErrProjectToolboxUnavailable, err))
	if !ok || category != "process_exit" || code != 125 {
		t.Fatalf("exit diagnostic=%s/%d/%t", category, code, ok)
	}
	wrapped := toolboxCommandError(err, []byte("secret=private: connect: permission denied /private/socket"))
	failure := toolboxInspectionError(ErrProjectToolboxOwnershipInspectUnavailable, ErrProjectToolboxUnavailable, wrapped)
	_, category, code, ok = ProjectToolboxFailureDiagnostic(failure)
	if !ok || category != "engine_permission_denied" || code != 125 || !errors.Is(failure, err) {
		t.Fatalf("engine diagnostic=%s/%d/%t", category, code, ok)
	}
	if strings.Contains(wrapped.Error(), "private") || strings.Contains(failure.Error(), "private") {
		t.Fatal("engine output leaked")
	}
}
