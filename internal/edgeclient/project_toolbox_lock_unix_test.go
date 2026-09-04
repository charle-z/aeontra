//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectToolboxWorkspaceLockRegistryReclaimsAfterWaiter(t *testing.T) {
	manager := &ProjectToolboxManager{stateRoot: filepath.Join(t.TempDir(), "state")}
	workspaceID := "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first, err := manager.acquireWorkspaceLock(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func(), 1)
	go func() {
		release, acquireErr := manager.acquireWorkspaceLock(context.Background(), workspaceID)
		if acquireErr != nil {
			return
		}
		acquired <- release
	}()
	first()
	var second func()
	select {
	case second = <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second waiter did not acquire after first release")
	}
	second()

	projectToolboxWorkspaceLocks.mu.Lock()
	defer projectToolboxWorkspaceLocks.mu.Unlock()
	if len(projectToolboxWorkspaceLocks.entries) != 0 {
		t.Fatalf("lock registry retained %d entries", len(projectToolboxWorkspaceLocks.entries))
	}
}

func TestProjectToolboxWorkspaceLockRegistryReclaimsCancelledWaiter(t *testing.T) {
	manager := &ProjectToolboxManager{stateRoot: filepath.Join(t.TempDir(), "state")}
	workspaceID := "ws_dddddddddddddddddddddddddddddddd"
	release, err := manager.acquireWorkspaceLock(context.Background(), workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.acquireWorkspaceLock(ctx, workspaceID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire err=%v", err)
	}
	release()

	projectToolboxWorkspaceLocks.mu.Lock()
	defer projectToolboxWorkspaceLocks.mu.Unlock()
	if len(projectToolboxWorkspaceLocks.entries) != 0 {
		t.Fatalf("cancelled waiter retained %d entries", len(projectToolboxWorkspaceLocks.entries))
	}
}

func TestProjectToolboxCleanupRetiringRemovesOnlyOwnedContainers(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	canonical := "mcp-toolbox-11111111111111111111111111111111"
	retiring := canonical + "-retiring-1"
	retiringID := strings.Repeat("a", 64)
	foreignID := strings.Repeat("b", 64)
	runner := &retiringCleanupRunner{
		base:       &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")},
		retiringID: retiringID,
		foreignID:  foreignID,
		retiring:   retiring,
	}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.base.socket, Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}); err != nil {
		t.Fatal(err)
	}
	record, err := manager.load(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.cleanupRetiringContainers(t.Context(), record, "project", "parrot", workspace); err != nil {
		t.Fatal(err)
	}
	if !runner.removed[retiringID] {
		t.Fatalf("owned retiring container was not removed: %+v", runner.removed)
	}
	if runner.removed[foreignID] {
		t.Fatalf("foreign container was removed: %+v", runner.removed)
	}
}

func TestProjectToolboxCleanupRetiringRefusesMismatchedMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_cccccccccccccccccccccccccccccccc", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	canonical := "mcp-toolbox-11111111111111111111111111111111"
	retiring := canonical + "-retiring-1"
	id := strings.Repeat("c", 64)
	runner := &retiringCleanupRunner{
		base:       &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")},
		retiringID: id, retiring: retiring, mismatched: true,
	}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot,
		Endpoint:  &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.base.socket, Executable: "/usr/bin/podman"},
		Runner:    runner, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}); err != nil {
		t.Fatal(err)
	}
	record, err := manager.load(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.cleanupRetiringContainers(t.Context(), record, "project", "parrot", workspace); !errors.Is(err, ErrProjectToolboxIdentityMismatch) {
		t.Fatalf("cleanup error=%v, want identity mismatch", err)
	}
	if runner.removed[id] {
		t.Fatalf("mismatched container was removed")
	}
}

type retiringCleanupRunner struct {
	base       *recordingToolboxRunner
	retiringID string
	foreignID  string
	retiring   string
	mismatched bool
	removed    map[string]bool
}

func (runner *retiringCleanupRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	joined := strings.Join(args, " ")
	if runner.removed == nil {
		runner.removed = map[string]bool{}
	}
	if strings.Contains(joined, " ps -aq ") {
		return []byte(runner.retiringID + "\n" + runner.foreignID + "\n"), nil
	}
	if strings.Contains(joined, " inspect ") && strings.Contains(joined, "{{.Name}}") {
		if strings.Contains(joined, runner.retiringID) {
			return []byte("/" + runner.retiring + "\n"), nil
		}
		return []byte("/foreign-container\n"), nil
	}
	if runner.mismatched && strings.Contains(joined, "Config.Labels") && strings.Contains(joined, runner.retiring) {
		return []byte("tb_foreign|sha256:" + strings.Repeat("a", 64) + "\n"), nil
	}
	if strings.Contains(joined, " rm -f ") {
		for _, id := range []string{runner.retiringID, runner.foreignID} {
			if strings.Contains(joined, id) {
				runner.removed[id] = true
			}
		}
		return nil, nil
	}
	return runner.base.Run(ctx, executable, args, environment)
}
