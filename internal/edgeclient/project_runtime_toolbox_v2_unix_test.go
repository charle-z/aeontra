//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProjectRuntimeRootsArePrivatePerWorkspace(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	workspace := Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	other := workspace
	other.ID = "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	roots, err := prepareProjectRuntimeRoots(stateRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	otherRoots, err := prepareProjectRuntimeRoots(stateRoot, other)
	if err != nil {
		t.Fatal(err)
	}
	if roots.Runtime == otherRoots.Runtime || roots.Cache == otherRoots.Cache || roots.Artifacts == otherRoots.Artifacts {
		t.Fatalf("workspace roots collided: %+v %+v", roots, otherRoots)
	}
	for _, root := range []string{roots.Runtime, roots.Cache, roots.Artifacts} {
		info, statErr := os.Lstat(root)
		if statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("root=%q info=%+v err=%v", root, info, statErr)
		}
		if pathInside(workspace.Path, root) || !pathInside(stateRoot, root) {
			t.Fatalf("root crossed boundary: state=%q source=%q root=%q", stateRoot, workspace.Path, root)
		}
	}
	link := filepath.Join(stateRoot, projectRuntimeStateDirectory, other.ID)
	_ = os.RemoveAll(link)
	if err := os.Symlink(roots.Runtime, link); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareProjectRuntimeRoots(stateRoot, other); err == nil {
		t.Fatal("runtime symlink was accepted")
	}
}

func TestProjectToolboxMigratesLegacyRecordWithoutDeletingWorkspace(t *testing.T) {
	stateRoot := t.TempDir()
	workspacePath := t.TempDir()
	workspace := Workspace{ID: "ws_cccccccccccccccccccccccccccccccc", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspacePath, socket: filepath.Join(stateRoot, "podman.sock")}
	runner.mounts = []struct {
		Type, Source, Destination string
		RW                        bool
	}{{Type: "bind", Source: workspacePath, Destination: "/workspace", RW: true}}
	runner.containerEnv = []string{"MCP_DEVBOX_TOOLBOX_CONTAINER_ACCESS=disabled"}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner: runner, environment: testRootlessContainerEnvironment,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	record := projectToolboxRecord{
		ToolboxID: "tb_11111111111111111111111111111111", WorkspaceID: workspace.ID, ProjectAlias: "project", TargetAlias: "parrot",
		ContainerName: "mcp-toolbox-11111111111111111111111111111111", BaseImage: projectToolboxBaseImage, BaseImageID: "sha256:" + strings.Repeat("a", 64),
		CreatedAt: now, UpdatedAt: now, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048,
	}
	if err := manager.save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Status(context.Background(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manager.recordPath(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	var migrated projectToolboxRecord
	if err := json.Unmarshal(data, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.SchemaVersion != projectToolboxSchemaVersion || migrated.RuntimeVersion != projectToolboxRuntimeV1 || migrated.Generation != 1 || migrated.WorkspaceFingerprint == "" || migrated.WorkspacePath != workspacePath {
		t.Fatalf("legacy migration=%+v", migrated)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("migration touched workspace: %v", err)
	}
}

func TestProjectToolboxReconcileReplacesOnlyProvenMountMismatch(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_dddddddddddddddddddddddddddddddd", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner: runner, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}
	if _, _, err := manager.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	// An unexpected mount is repairable only after the container's label,
	// image, resource limits and environment still prove ownership.
	runner.mounts = []struct {
		Type, Source, Destination string
		RW                        bool
	}{{Type: "bind", Source: workspace.Path, Destination: "/workspace", RW: true}}
	snapshot, err := manager.Reconcile(context.Background(), ProjectToolboxReconcileRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || snapshot.State != ProjectToolboxRunning {
		t.Fatalf("reconciled snapshot=%+v err=%v", snapshot, err)
	}
	if !strings.Contains(strings.Join(runner.calls[len(runner.calls)-1], " "), "inspect") {
		t.Fatalf("reconcile did not re-attest replacement: calls=%v", runner.calls)
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), " git reset") {
			t.Fatal("reconcile attempted to mutate source")
		}
	}

	other := workspace
	other.Path = t.TempDir()
	_, err = manager.Status(context.Background(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: other})
	if !errors.Is(err, ErrProjectToolboxIdentityMismatch) {
		t.Fatalf("repository replacement error=%v", err)
	}
}

func TestProjectToolboxReconcileRestoresOldContainerWhenReplacementFails(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) {
			return "tb_11111111111111111111111111111111", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}
	if _, _, err := manager.Create(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before, err := manager.loadForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	runner.mounts = []struct {
		Type, Source, Destination string
		RW                        bool
	}{{Type: "bind", Source: filepath.Join(stateRoot, "wrong"), Destination: "/workspace", RW: true}}
	runner.fail = "create --name"
	_, err = manager.Reconcile(context.Background(), ProjectToolboxReconcileRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if !errors.Is(err, ErrProjectToolboxUnavailable) {
		t.Fatalf("replacement error=%v", err)
	}
	after, err := manager.loadForWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation != before.Generation || after.ContainerName != before.ContainerName {
		t.Fatalf("failed replacement changed record: before=%+v after=%+v", before, after)
	}
	calls := make([]string, len(runner.calls))
	for i, call := range runner.calls {
		calls[i] = strings.Join(call, " ")
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rename "+before.ContainerName+" "+before.ContainerName+"-retiring-") {
		t.Fatalf("old container was not retired for rollback: calls=%v", calls)
	}
	if len(calls) < 2 || !strings.Contains(calls[len(calls)-2], "rename "+before.ContainerName+"-retiring-"+strconv.FormatUint(before.Generation, 10)+" "+before.ContainerName) ||
		!strings.Contains(calls[len(calls)-1], "start "+before.ContainerName) {
		t.Fatalf("old container was not restored and restarted last: calls=%v", calls)
	}
}
