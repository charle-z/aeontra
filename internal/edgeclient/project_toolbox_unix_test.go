//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordingToolboxRunner struct {
	calls          [][]string
	execExitCode   int
	environments   [][]string
	fail           string
	failPatterns   []string
	workspace      string
	socket         string
	state          string
	sizeOutput     string
	inspectImageID string
	harnessState   string
	psLabelOutput  string
	psNameOutput   string
	mounts         []struct {
		Type, Source, Destination string
		RW                        bool
	}
	containerEnv []string
}

func (runner *recordingToolboxRunner) Run(_ context.Context, executable string, args, environment []string) ([]byte, error) {
	call := append([]string{executable}, args...)
	runner.calls = append(runner.calls, call)
	runner.environments = append(runner.environments, append([]string(nil), environment...))
	joined := strings.Join(args, " ")
	if runner.fail != "" && strings.Contains(joined, runner.fail) {
		return nil, errors.New("runner failed")
	}
	for _, pattern := range runner.failPatterns {
		if strings.Contains(joined, pattern) {
			return nil, errors.New("runner failed")
		}
	}
	switch {
	case strings.Contains(joined, " ps -aq ") && strings.Contains(joined, "label="+projectToolboxLabelKey+"="):
		return []byte(runner.psLabelOutput), nil
	case strings.Contains(joined, " ps -aq ") && strings.Contains(joined, "name="):
		return []byte(runner.psNameOutput), nil
	case strings.Contains(joined, "image inspect"):
		return []byte("sha256:" + strings.Repeat("a", 64) + "\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "Config.Labels"):
		imageID := runner.inspectImageID
		if imageID == "" {
			imageID = "sha256:" + strings.Repeat("a", 64)
		}
		return []byte("tb_11111111111111111111111111111111|" + imageID + "\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "json .Mounts"):
		encoded, _ := json.Marshal(runner.mounts)
		return encoded, nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "HostConfig.Memory"):
		return []byte("8589934592|4000000000|2048\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "json .Config.Env"):
		encoded, _ := json.Marshal(runner.containerEnv)
		return encoded, nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "SizeRw"):
		if runner.sizeOutput != "" {
			return []byte(runner.sizeOutput), nil
		}
		return []byte("4096|83886080\n"), nil
	case strings.Contains(joined, " inspect "):
		if runner.state == "" {
			runner.state = "running|true"
		}
		return []byte(runner.state + "\n"), nil
	case strings.Contains(joined, " create "):
		runner.mounts = nil
		runner.containerEnv = nil
		for index := 0; index+1 < len(args); index++ {
			switch args[index] {
			case "--volume":
				parts := strings.Split(args[index+1], ":")
				if len(parts) == 3 {
					runner.mounts = append(runner.mounts, struct {
						Type, Source, Destination string
						RW                        bool
					}{Type: "bind", Source: parts[0], Destination: parts[1], RW: parts[2] == "rw"})
				}
			case "--env":
				runner.containerEnv = append(runner.containerEnv, args[index+1])
			}
		}
		return []byte(strings.Repeat("b", 64) + "\n"), nil
	case strings.Contains(joined, "mcp-browser-harness-start"):
		return nil, nil
	case strings.Contains(joined, "mcp-browser-harness-status"):
		state := runner.harnessState
		if state == "" {
			state = "running"
		}
		return []byte(state + "\n"), nil
	case strings.Contains(joined, "mcp-browser-harness-stop"):
		runner.harnessState = "stopped"
		return []byte("stopped\n"), nil
	case strings.Contains(joined, "mcp-toolbox-service-start"):
		return nil, nil
	case strings.Contains(joined, "mcp-toolbox-service-status"):
		return []byte("running\n"), nil
	case strings.Contains(joined, "mcp-toolbox-service-stop"):
		return []byte("stopped\n"), nil
	case strings.Contains(joined, " exec "):
		if runner.execExitCode != 0 {
			command := exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(runner.execExitCode))
			return []byte("make: *** test failed\n"), command.Run()
		}
		return []byte("toolbox-ok\n"), nil
	case strings.Contains(joined, " start "):
		runner.state = "running|true"
		return nil, nil
	default:
		return nil, nil
	}
}

func TestProjectToolboxServiceLifecycleAndRepairUseOwnedContainer(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:    stateRoot,
		Endpoint:     &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:       runner,
		environment:  testRootlessContainerEnvironment,
		NewID:        func() (string, error) { return "tb_11111111111111111111111111111111", nil },
		NewServiceID: func() (string, error) { return "ts_33333333333333333333333333333333", nil },
		Now:          func() time.Time { return time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	service, reused, err := manager.ServiceStart(t.Context(), ProjectToolboxServiceStartRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, Name: "preview",
		Argv: []string{"python3", "-m", "http.server", "8080"}, CWD: "public", Environment: map[string]string{"PORT": "8080"},
	})
	if err != nil || reused || service.ServiceID != "ts_33333333333333333333333333333333" || service.State != "running" {
		t.Fatalf("service=%+v reused=%v err=%v", service, reused, err)
	}
	var startCall string
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "mcp-toolbox-service-start") {
			startCall = joined
		}
	}
	if !strings.Contains(startCall, " exec --detach --workdir /workspace/public --env PORT=8080 mcp-toolbox-") || !strings.HasSuffix(startCall, " ts_33333333333333333333333333333333 python3 -m http.server 8080") {
		t.Fatalf("start call=%q", startCall)
	}
	status, err := manager.ServiceStatus(t.Context(), ProjectToolboxServiceRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, ServiceID: service.ServiceID})
	if err != nil || status.State != "running" || status.Name != "preview" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	stopped, err := manager.ServiceStop(t.Context(), ProjectToolboxServiceRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, ServiceID: service.ServiceID})
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
	for _, marker := range []string{"mcp-toolbox-service-status", "mcp-toolbox-service-stop"} {
		var controlCall string
		for _, call := range runner.calls {
			joined := strings.Join(call, " ")
			if strings.Contains(joined, marker) {
				controlCall = joined
			}
		}
		if !strings.Contains(controlCall, `*[!0-9:]*`) || !strings.Contains(controlCall, `/proc/$pid/stat`) {
			t.Fatalf("%s did not validate numeric pid/start ticks: %q", marker, controlCall)
		}
	}
	runner.state = "exited|false"
	startCalls := countToolboxCalls(runner.calls, " start ")
	status, err = manager.ServiceStatus(t.Context(), ProjectToolboxServiceRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, ServiceID: service.ServiceID})
	if err != nil || status.State != "stopped" || countToolboxCalls(runner.calls, " start ") != startCalls {
		t.Fatalf("stopped status=%+v err=%v calls=%v", status, err, runner.calls)
	}
	repaired, err := manager.Repair(t.Context(), ProjectToolboxRepairRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || repaired.State != ProjectToolboxRunning {
		t.Fatalf("repaired=%+v err=%v", repaired, err)
	}
}

func countToolboxCalls(calls [][]string, fragment string) int {
	count := 0
	for _, call := range calls {
		if strings.Contains(" "+strings.Join(call, " ")+" ", fragment) {
			count++
		}
	}
	return count
}

func TestProjectToolboxPersistsRootlessContainerAndExecutesArbitraryArgv(t *testing.T) {
	stateRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	runner := &recordingToolboxRunner{workspace: workspaceRoot, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
		Now:         func() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: workspaceRoot, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	created, reused, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if reused || created.ToolboxID != "tb_11111111111111111111111111111111" || created.State != ProjectToolboxRunning || created.BaseImageID != "sha256:"+strings.Repeat("a", 64) || created.CPUMillis != 4000 || created.MemoryMiB != 8192 || created.ProcessLimit != 2048 || created.Lifecycle != projectToolboxPersistent || created.Generation != 1 || created.Reclaimable || created.ReclaimReason != "persistent" {
		t.Fatalf("created=%+v reused=%v", created, reused)
	}
	var createCall string
	for _, call := range runner.calls {
		if strings.Contains(" "+strings.Join(call, " ")+" ", " create ") {
			createCall = strings.Join(call, " ")
		}
	}
	if !containsToolboxArgs(createCall, "--cpus 4.000", "--memory 8192m", "--pids-limit 2048", "--env MCP_DEVBOX_TOOLBOX_CONTAINER_ACCESS=disabled", "--volume "+workspaceRoot+":/workspace:rw") {
		t.Fatalf("create call=%q", createCall)
	}
	for _, forbidden := range []string{"container.sock", "DOCKER_HOST", "CONTAINER_HOST", "MCP_DEVBOX_CONTAINER_ENGINE", "MCP_DEVBOX_CONTAINER_LABEL", "COMPOSE_PROJECT_NAME"} {
		if strings.Contains(createCall, forbidden) {
			t.Fatalf("create call exposes container authority %q: %q", forbidden, createCall)
		}
	}
	if created.ContainerAccess || created.WritableBytes != 4096 || created.RootFSBytes != 80<<20 {
		t.Fatalf("storage/container metadata=%+v", created)
	}
	if info, err := os.Lstat(filepath.Join(stateRoot, projectToolboxStateDirectory, workspace.ID+".json")); err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("metadata info=%+v err=%v", info, err)
	}

	manager, err = OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: manager.endpoint, Runner: runner, environment: testRootlessContainerEnvironment})
	if err != nil {
		t.Fatal(err)
	}
	runner.state = "exited|false"
	status, reused, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048})
	if err != nil || !reused || status.ToolboxID != created.ToolboxID || status.State != ProjectToolboxRunning {
		t.Fatalf("recovered status=%+v reused=%v err=%v", status, reused, err)
	}
	executed, err := manager.Exec(t.Context(), ProjectToolboxExecRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
		Argv: []string{"sh", "-lc", "command -v ruby || true"}, CWD: "src", Environment: map[string]string{"CI": "true"},
	})
	if err != nil || executed.Output != "toolbox-ok\n" || executed.State != ProjectToolboxRunning {
		t.Fatalf("executed=%+v err=%v", executed, err)
	}
	last := runner.calls[len(runner.calls)-1]
	wantTail := []string{"exec", "--workdir", "/workspace/src", "--env", "CI=true", "mcp-toolbox-11111111111111111111111111111111", "sh", "-lc", "command -v ruby || true"}
	if len(last) < len(wantTail) || !reflect.DeepEqual(last[len(last)-len(wantTail):], wantTail) {
		t.Fatalf("exec call=%q", last)
	}
	runner.execExitCode = 2
	failed, err := manager.Exec(t.Context(), ProjectToolboxExecRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, Argv: []string{"make", "test"},
	})
	if err != nil || failed.Output != "make: *** test failed\n" || failed.ExitCode == nil || *failed.ExitCode != 2 {
		t.Fatalf("failed command result=%+v err=%v", failed, err)
	}
}

func containsToolboxArgs(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}

func TestProjectToolboxRejectsLimitDriftOnReuse(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"}, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}
	if _, _, err := manager.Create(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	request.MemoryMiB = 16384
	if _, _, err := manager.Create(t.Context(), request); !errors.Is(err, ErrProjectToolboxUnsafeState) {
		t.Fatalf("limit drift err=%v", err)
	}
	request.MemoryMiB = 8192
	request.Lifecycle = projectToolboxDisposable
	if _, _, err := manager.Create(t.Context(), request); !errors.Is(err, ErrProjectToolboxUnsafeState) {
		t.Fatalf("lifecycle drift err=%v", err)
	}
}

func TestProjectToolboxRejectsCrossProjectAccessAndCleansUpOnlyExplicitly(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "other", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxNotOwned) {
		t.Fatalf("cross-project err=%v", err)
	}
	removed, err := manager.Cleanup(t.Context(), ProjectToolboxCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, projectToolboxStateDirectory, workspace.ID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata survived cleanup: %v", err)
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if !strings.Contains(last, " rm -f mcp-toolbox-11111111111111111111111111111111") {
		t.Fatalf("cleanup call=%q", last)
	}
}

func TestProjectToolboxStatusDoesNotPersistEndpointRefresh(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_44444444444444444444444444444444", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: endpoint, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	record, err := manager.load(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.EndpointFingerprint = projectRuntimeEndpointFingerprint(&RootlessContainerEndpoint{Engine: "docker", SocketPath: filepath.Join(stateRoot, "docker.sock"), Executable: "/usr/bin/docker"})
	record.Generation = 7
	record.UpdatedAt = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	if err := manager.save(record); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(manager.recordPath(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(manager.recordPath(workspace.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("status rewrote toolbox metadata\nbefore=%s\nafter=%s", before, after)
	}
	if snapshot.Generation != record.Generation || !snapshot.UpdatedAt.Equal(record.UpdatedAt) {
		t.Fatalf("status reported unpersisted metadata: snapshot=%+v record=%+v", snapshot, record)
	}
}

func TestProjectToolboxStatusDoesNotCreateOrRepairRuntimeRoots(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string) error
		check  func(*testing.T, string)
	}{
		{
			name:   "missing runtime root",
			mutate: func(root string) error { return os.RemoveAll(root) },
			check: func(t *testing.T, root string) {
				if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("status recreated missing runtime root: info/error=%v", err)
				}
			},
		},
		{
			name:   "incorrect runtime root permissions",
			mutate: func(root string) error { return os.Chmod(root, 0o755) },
			check: func(t *testing.T, root string) {
				info, err := os.Lstat(root)
				if err != nil || info.Mode().Perm() != 0o755 {
					t.Fatalf("status repaired runtime root permissions: info=%+v err=%v", info, err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			workspace := Workspace{ID: "ws_66666666666666666666666666666666", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
			runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
			manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"}, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
				t.Fatal(err)
			}
			runtimeRoot := filepath.Join(filepath.Dir(manager.stateRoot), projectRuntimeStateDirectory, workspace.ID)
			if err := test.mutate(runtimeRoot); err != nil {
				t.Fatal(err)
			}
			_, err = manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
			if !errors.Is(err, ErrProjectToolboxMountMismatch) {
				t.Fatalf("status err=%v want %v", err, ErrProjectToolboxMountMismatch)
			}
			test.check(t, runtimeRoot)
		})
	}
}

func TestProjectToolboxStatusDistinguishesMissingContainerFromEngineFailure(t *testing.T) {
	for _, test := range []struct {
		name           string
		failPatterns   []string
		psLabelOutput  string
		sizeOutput     string
		want           error
		wantCause      error
		wantLabelQuery bool
		wantNameQuery  bool
	}{
		{name: "missing owned container", failPatterns: []string{"inspect --format {{index .Config.Labels"}, want: ErrProjectToolboxContainerMissing, wantLabelQuery: true, wantNameQuery: true},
		{name: "engine unavailable during scoped probe", failPatterns: []string{"inspect --format {{index .Config.Labels", "ps -aq"}, want: ErrProjectToolboxOwnershipInspectUnavailable, wantCause: ErrProjectToolboxUnavailable, wantLabelQuery: true},
		{name: "labelled candidate is not declared missing", failPatterns: []string{"inspect --format {{index .Config.Labels"}, psLabelOutput: strings.Repeat("c", 64), want: ErrProjectToolboxOwnershipInspectUnavailable, wantCause: ErrProjectToolboxContainerUnavailable, wantLabelQuery: true},
		{name: "state inspect unavailable during scoped probe", failPatterns: []string{".State.Status", "ps -aq"}, want: ErrProjectToolboxStateInspectUnavailable, wantCause: ErrProjectToolboxUnavailable, wantLabelQuery: true},
		{name: "state inspect finds a candidate", failPatterns: []string{".State.Status"}, psLabelOutput: strings.Repeat("c", 64), want: ErrProjectToolboxStateInspectUnavailable, wantCause: ErrProjectToolboxContainerUnavailable, wantLabelQuery: true},
		{name: "state inspect confirms missing", failPatterns: []string{".State.Status"}, want: ErrProjectToolboxContainerMissing, wantLabelQuery: true, wantNameQuery: true},
		{name: "storage inspect unavailable", failPatterns: []string{"--size --format"}, want: ErrProjectToolboxStorageInspectUnavailable, wantCause: ErrProjectToolboxUnavailable},
		{name: "storage inspect malformed", sizeOutput: "not-a-size|83886080\n", want: ErrProjectToolboxStorageInspectUnavailable, wantCause: ErrProjectToolboxUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			workspace := Workspace{ID: "ws_55555555555555555555555555555555", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
			runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
			manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"}, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
				t.Fatal(err)
			}
			runner.calls = nil
			runner.failPatterns = test.failPatterns
			runner.psLabelOutput = test.psLabelOutput
			runner.sizeOutput = test.sizeOutput
			_, err = manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
			if !errors.Is(err, test.want) {
				t.Fatalf("status err=%v want %v", err, test.want)
			}
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("status err=%v does not preserve cause %v", err, test.wantCause)
			}
			var labelQuery, nameQuery bool
			for _, call := range runner.calls {
				joined := strings.Join(call, " ")
				labelQuery = labelQuery || strings.Contains(joined, "ps -aq --filter label="+projectToolboxLabelKey+"=tb_11111111111111111111111111111111")
				nameQuery = nameQuery || strings.Contains(joined, "ps -aq --filter name=mcp-toolbox-11111111111111111111111111111111")
			}
			if labelQuery != test.wantLabelQuery || nameQuery != test.wantNameQuery {
				t.Fatalf("status used unexpected scoped probes: label=%t name=%t wantLabel=%t wantName=%t calls=%q", labelQuery, nameQuery, test.wantLabelQuery, test.wantNameQuery, runner.calls)
			}
			for _, call := range runner.calls {
				joined := strings.Join(call, " ")
				if strings.Contains(joined, " rename ") || strings.Contains(joined, " rm ") {
					t.Fatalf("read-only status mutated container state: %q", joined)
				}
			}
		})
	}
}

func TestProjectToolboxStatusKeepsUnknownStateNonReclaimable(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_55555555555555555555555555555555", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"}, Runner: runner, environment: testRootlessContainerEnvironment, NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil }})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, Lifecycle: projectToolboxDisposable}
	if _, _, err := manager.Create(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	runner.state = "paused|true"
	snapshot, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State != ProjectToolboxUnknown || snapshot.Reclaimable || snapshot.ReclaimReason != "unknown_state" {
		t.Fatalf("unknown status state=%q reclaimable=%t reason=%q", snapshot.State, snapshot.Reclaimable, snapshot.ReclaimReason)
	}
}

func TestProjectToolboxFailsClosedOnUnsafeStateAndMissingRootlessEngine(t *testing.T) {
	if _, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: t.TempDir()}); !errors.Is(err, ErrProjectToolboxUnavailable) {
		t.Fatalf("missing endpoint err=%v", err)
	}
	stateRoot := t.TempDir()
	stateDir := filepath.Join(stateRoot, projectToolboxStateDirectory)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "ws_22222222222222222222222222222222.json")
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), path); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:      &recordingToolboxRunner{},
		environment: testRootlessContainerEnvironment,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	if _, err := manager.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
		t.Fatalf("unsafe state err=%v", err)
	}
}

func TestNormalizeProjectToolboxImageIDAcceptsDockerAndPodmanForms(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, input := range []string{digest, "sha256:" + digest} {
		normalized, err := normalizeProjectToolboxImageID(input)
		if err != nil || normalized != "sha256:"+digest {
			t.Fatalf("input=%q normalized=%q err=%v", input, normalized, err)
		}
	}
	for _, input := range []string{"", "sha256:", strings.Repeat("a", 63), strings.Repeat("A", 64), "sha512:" + digest} {
		if _, err := normalizeProjectToolboxImageID(input); err == nil {
			t.Fatalf("unsafe image identity accepted: %q", input)
		}
	}
}

func TestProjectToolboxOwnershipAcceptsBarePodmanImageIdentity(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{
		workspace:      workspace.Path,
		socket:         filepath.Join(stateRoot, "podman.sock"),
		inspectImageID: strings.Repeat("a", 64),
	}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace})
	if err != nil || created.State != ProjectToolboxRunning {
		t.Fatalf("created=%+v err=%v", created, err)
	}
}

func TestProjectToolboxManagerUsesValidatedRootlessSocketForPullAndCreate(t *testing.T) {
	runtimeRoot, socketPath := testOwnedRootlessSocket(t)
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: socketPath}
	environment := func(endpoint *RootlessContainerEndpoint, toolPath string) ([]string, error) {
		return rootlessContainerClientEnvironmentFor(endpoint, toolPath, runtimeRoot, os.Geteuid())
	}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   t.TempDir(),
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: socketPath, Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: environment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}

	wantEndpoint := "unix://" + socketPath
	pullSeen, createSeen := false, false
	for index, call := range runner.calls {
		joined := " " + strings.Join(call, " ") + " "
		if !strings.Contains(joined, " pull ") && !strings.Contains(joined, " create ") {
			continue
		}
		values := environmentMap(runner.environments[index])
		if values["CONTAINER_HOST"] != wantEndpoint || values["DOCKER_HOST"] != wantEndpoint || values["XDG_RUNTIME_DIR"] != runtimeRoot {
			t.Fatalf("command=%q environment=%q", call, runner.environments[index])
		}
		if strings.Contains(strings.Join(runner.environments[index], "\n"), "/var/run/docker.sock") {
			t.Fatalf("rootful fallback in command environment: %q", runner.environments[index])
		}
		if strings.Contains(joined, " pull ") {
			pullSeen = true
		}
		if strings.Contains(joined, " create ") {
			createSeen = true
			if strings.Contains(joined, socketPath+":") || strings.Contains(joined, "container.sock") || strings.Contains(joined, "/var/run/docker.sock") {
				t.Fatalf("create authority=%q", call)
			}
		}
	}
	if !pullSeen || !createSeen {
		t.Fatalf("pull/create not observed: %v", runner.calls)
	}
}

func TestProjectToolboxRejectsContainerEngineAuthorityEnvironmentOverrides(t *testing.T) {
	manager, runner, workspace := testBrowserHarnessManager(t)
	cases := map[string]string{
		"CONTAINER_HOST":               "unix:///var/run/docker.sock",
		"DOCKER_HOST":                  "unix:///var/run/docker.sock",
		"CONTAINERS_HELPER_BINARY_DIR": "/workspace/untrusted-helpers",
		"CONTAINERS_CONF":              "/workspace/untrusted-containers.conf",
		"CONTAINERS_CONF_OVERRIDE":     "/workspace/untrusted-override.conf",
		"CONTAINERS_CONF_MODULES":      "/workspace/untrusted-module.conf",
		"CONTAINERS_STORAGE_CONF":      "/workspace/untrusted-storage.conf",
	}
	for key, value := range cases {
		if _, err := manager.Exec(t.Context(), ProjectToolboxExecRequest{
			ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
			Argv: []string{"true"}, Environment: map[string]string{key: value},
		}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("exec override %s err=%v", key, err)
		}
		if _, _, err := manager.ServiceStart(t.Context(), ProjectToolboxServiceStartRequest{
			ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
			Name: "override-" + strings.ToLower(strings.ReplaceAll(key, "_", "-")), Argv: []string{"sleep", "1"}, Environment: map[string]string{key: value},
		}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("service override %s err=%v", key, err)
		}
		if _, _, err := manager.BrowserHarnessStart(t.Context(), ProjectBrowserHarnessStartRequest{
			ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
			IdempotencyKey: "override-" + strings.ToLower(strings.ReplaceAll(key, "_", "-")), Profile: "default",
			Argv: []string{"true"}, Environment: map[string]string{key: value}, TimeoutSeconds: 60, StorageMiB: 128,
		}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("browser harness override %s err=%v", key, err)
		}
	}
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "/var/run/docker.sock") || strings.Contains(joined, "untrusted-") {
			t.Fatalf("rejected container authority reached runner: %q", call)
		}
	}
}

func TestProjectToolboxUsesOnlyValidatedLocalCgroupParent(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	const parent = "/system.slice/p12-rootless-podman-12345-1-2.service/containers"
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot: stateRoot, Endpoint: &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"},
		Runner: runner, environment: testRootlessContainerEnvironment, cgroupParent: parent,
		NewID: func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(t.Context(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	var create string
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(" "+joined+" ", " create ") {
			create = joined
		}
	}
	if !strings.Contains(create, "--cgroup-parent "+parent) {
		t.Fatalf("create call=%q", create)
	}
	for _, invalid := range []string{"/user.slice/user-1000.slice", "/system.slice/docker.service", parent + "/child", "../containers"} {
		if _, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: t.TempDir(), Endpoint: manager.endpoint, Runner: runner, environment: testRootlessContainerEnvironment, cgroupParent: invalid}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("invalid cgroup parent %q err=%v", invalid, err)
		}
	}
}
