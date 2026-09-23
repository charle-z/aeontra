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
	base            *recordingToolboxRunner
	expectedName    string
	candidateID     string
	nameCandidateID string
	candidateName   string
	psFailure       bool
	recovered       bool
}

func (runner *ownedToolboxRecoveryRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, " ps -aq ") && strings.Contains(joined, "label="+projectToolboxLabelKey+"=tb_11111111111111111111111111111111"):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		if runner.psFailure {
			return nil, errors.New("engine unavailable")
		}
		if runner.candidateID == "" {
			return nil, nil
		}
		return []byte(runner.candidateID + "\n"), nil
	case strings.Contains(joined, " ps -aq ") && strings.Contains(joined, "name=mcp-toolbox-11111111111111111111111111111111"):
		_, _ = runner.base.Run(ctx, executable, args, environment)
		if runner.psFailure {
			return nil, errors.New("engine unavailable")
		}
		if runner.nameCandidateID == "" {
			return nil, nil
		}
		return []byte(runner.nameCandidateID + "\n"), nil
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

func TestProjectToolboxReconcileReportsMissingContainer(t *testing.T) {
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
	if _, err := manager.Reconcile(t.Context(), ProjectToolboxReconcileRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxContainerMissing) {
		t.Fatalf("reconcile err=%v", err)
	}
	if countToolboxCalls(base.calls, " create ") != createsBefore {
		t.Fatal("reconcile rebuilt a missing toolbox")
	}
}

func TestProjectToolboxCleanupMissingRecordAllowsFreshCreate(t *testing.T) {
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
	manager.runner = &ownedToolboxRecoveryRunner{base: base, expectedName: "mcp-toolbox-11111111111111111111111111111111"}
	snapshot, removed, err := manager.CleanupMissing(t.Context(), ProjectToolboxCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || !removed {
		t.Fatalf("removed=%t err=%v", removed, err)
	}
	if snapshot.ToolboxID != "tb_11111111111111111111111111111111" || snapshot.State != "removed" || snapshot.RootFSBytes != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if _, err := manager.load(workspace.ID); !errors.Is(err, ErrProjectToolboxNotFound) {
		t.Fatalf("record remains: %v", err)
	}
	manager.runner = base
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatalf("fresh create: %v", err)
	}
}

func TestProjectToolboxCleanupMissingRecordFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*ProjectToolboxManager, *ProjectToolboxCleanupRequest, *ownedToolboxRecoveryRunner)
	}{
		{name: "labelled container", change: func(_ *ProjectToolboxManager, _ *ProjectToolboxCleanupRequest, runner *ownedToolboxRecoveryRunner) {
			runner.candidateID = strings.Repeat("c", 64)
		}},
		{name: "named container", change: func(_ *ProjectToolboxManager, _ *ProjectToolboxCleanupRequest, runner *ownedToolboxRecoveryRunner) {
			runner.nameCandidateID = strings.Repeat("c", 64)
		}},
		{name: "engine unavailable", change: func(_ *ProjectToolboxManager, _ *ProjectToolboxCleanupRequest, runner *ownedToolboxRecoveryRunner) {
			runner.psFailure = true
		}},
		{name: "endpoint changed", change: func(manager *ProjectToolboxManager, _ *ProjectToolboxCleanupRequest, _ *ownedToolboxRecoveryRunner) {
			manager.endpoint.SocketPath += "-other"
		}},
		{name: "wrong project", change: func(_ *ProjectToolboxManager, request *ProjectToolboxCleanupRequest, _ *ownedToolboxRecoveryRunner) {
			request.ProjectAlias = "other"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
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
			runner := &ownedToolboxRecoveryRunner{base: base, expectedName: "mcp-toolbox-11111111111111111111111111111111"}
			manager.runner = runner
			request := ProjectToolboxCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}
			test.change(manager, &request, runner)
			_, removed, err := manager.CleanupMissing(t.Context(), request)
			if removed || err == nil {
				t.Fatalf("removed=%t err=%v", removed, err)
			}
			if _, err := manager.load(workspace.ID); err != nil {
				t.Fatalf("record removed: %v", err)
			}
			if countToolboxCalls(base.calls, " rm ") != 0 {
				t.Fatal("container removal attempted")
			}
		})
	}
}
