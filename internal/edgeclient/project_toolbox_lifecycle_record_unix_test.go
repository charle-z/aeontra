//go:build !windows

package edgeclient

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectToolboxV3RecordRequiresLifecycleAndGeneration(t *testing.T) {
	for _, test := range []struct {
		name       string
		lifecycle  string
		generation uint64
	}{
		{name: "missing lifecycle", generation: 1},
		{name: "missing generation", lifecycle: projectToolboxPersistent},
		{name: "invalid lifecycle", lifecycle: "temporary", generation: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			workspace := Workspace{
				ID: "ws_88888888888888888888888888888888", Path: t.TempDir(),
				Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
			}
			manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
				StateRoot: stateRoot,
				Endpoint: &RootlessContainerEndpoint{
					Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman",
				},
				Runner:      &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")},
				environment: testRootlessContainerEnvironment,
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
			record := projectToolboxRecord{
				SchemaVersion: projectToolboxSchemaVersion, RuntimeVersion: projectToolboxRuntimeV2,
				Generation: test.generation, Lifecycle: test.lifecycle,
				ToolboxID: "tb_88888888888888888888888888888888", WorkspaceID: workspace.ID,
				ProjectAlias: "project", TargetAlias: "parrot",
				ContainerName: "mcp-toolbox-88888888888888888888888888888888",
				BaseImage:     projectToolboxBaseImage, BaseImageID: "sha256:" + strings.Repeat("a", 64),
				CreatedAt: now, UpdatedAt: now, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048,
			}
			if err := manager.save(record); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.loadForWorkspace(workspace); !errors.Is(err, ErrProjectToolboxUnsafeState) {
				t.Fatalf("unsafe v3 record accepted: %+v err=%v", record, err)
			}
		})
	}
}
