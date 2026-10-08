//go:build !windows

package edgeclient

import (
	"path/filepath"
	"testing"
)

func TestProjectToolboxDisposableLifecyclePersistsAndBecomesEligibleOnlyWhenStopped(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{
		ID: "ws_99999999999999999999999999999999", Path: t.TempDir(),
		Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
	}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot,
		Endpoint: &RootlessContainerEndpoint{
			Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman",
		},
		Runner: runner, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectToolboxCreateRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
		Lifecycle: projectToolboxDisposable,
	}
	created, reused, err := manager.Create(t.Context(), request)
	if err != nil || reused {
		t.Fatalf("created=%+v reused=%t err=%v", created, reused, err)
	}
	if created.Lifecycle != projectToolboxDisposable || created.Generation != 1 || created.Reclaimable || created.ReclaimReason != "active" {
		t.Fatalf("running disposable=%+v", created)
	}

	runner.state = "exited|false"
	stopped, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != ProjectToolboxStopped || stopped.Lifecycle != projectToolboxDisposable || stopped.Generation != 1 || !stopped.Reclaimable || stopped.ReclaimReason != "eligible" {
		t.Fatalf("stopped disposable=%+v", stopped)
	}
}
