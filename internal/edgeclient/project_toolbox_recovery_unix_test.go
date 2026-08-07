//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type ownedToolboxRecoveryRunner struct {
	base          *recordingToolboxRunner
	expectedName  string
	candidateID   string
	candidateName string
	recovered     bool
}

func (runner *ownedToolboxRecoveryRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " ps -aq ") && strings.Contains(joined, "label="+projectToolboxLabelKey+"=tb_11111111111111111111111111111111"):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		if runner.candidateID == "" {
			return nil, nil
		}
		return []byte(runner.candidateID + "\n"), nil
	case !runner.recovered && strings.Contains(joined, " inspect ") && strings.Contains(joined, runner.expectedName):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		return nil, errors.New("name lookup failed")
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "{{.Name}}"):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		return []byte(runner.candidateName + "\n"), nil
	case strings.Contains(joined, " rename "):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		runner.recovered = true
		return nil, nil
	default:
		return runner.base.Run(ctx, executable, args, environment)
	}
}

func TestProjectToolboxRepairRecoversUniqueOwnedLabelledContainerWithoutRebuild(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	base := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot,
		Endpoint:  &RootlessContainerEndpoint{Engine: "podman", SocketPath: base.socket, Executable: "/usr/bin/podman"},
		Runner:    base, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	createsBefore := countToolboxCalls(base.calls, " create ")
	expectedName := "mcp-toolbox-11111111111111111111111111111111"
	candidateID := strings.Repeat("c", 64)
	recovery := &ownedToolboxRecoveryRunner{base: base, expectedName: expectedName, candidateID: candidateID, candidateName: "legacy-toolbox-name"}
	manager.runner = recovery

	if _, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxContainerUnavailable) {
		t.Fatalf("pre-repair status err=%v", err)
	}
	snapshot, err := manager.Repair(t.Context(), ProjectToolboxRepairRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || snapshot.State != ProjectToolboxRunning || !recovery.recovered {
		t.Fatalf("snapshot=%+v recovered=%t err=%v", snapshot, recovery.recovered, err)
	}
	if countToolboxCalls(base.calls, " create ") != createsBefore {
		t.Fatal("repair rebuilt the toolbox instead of preserving its writable layer")
	}
	var rename string
	for _, call := range base.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(" "+joined+" ", " rename ") {
			rename = joined
		}
	}
	if !strings.Contains(rename, " rename "+candidateID+" "+expectedName) {
		t.Fatalf("rename=%q", rename)
	}
}

func TestProjectToolboxRepairProvesOwnedContainerMissingBeforeRefusingRebuild(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	base := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot,
		Endpoint:  &RootlessContainerEndpoint{Engine: "podman", SocketPath: base.socket, Executable: "/usr/bin/podman"},
		Runner:    base, environment: testRootlessContainerEnvironment,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	createsBefore := countToolboxCalls(base.calls, " create ")
	manager.runner = &ownedToolboxRecoveryRunner{base: base, expectedName: "mcp-toolbox-11111111111111111111111111111111"}
	if _, err := manager.Repair(t.Context(), ProjectToolboxRepairRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxContainerMissing) {
		t.Fatalf("repair err=%v", err)
	}
	if countToolboxCalls(base.calls, " create ") != createsBefore {
		t.Fatal("missing owned container triggered a destructive rebuild")
	}
}
