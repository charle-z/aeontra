//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingToolboxRunner struct {
	calls          [][]string
	environments   [][]string
	fail           string
	workspace      string
	socket         string
	state          string
	inspectImageID string
	harnessState   string
}

func (runner *recordingToolboxRunner) Run(_ context.Context, executable string, args, environment []string) ([]byte, error) {
	call := append([]string{executable}, args...)
	runner.calls = append(runner.calls, call)
	runner.environments = append(runner.environments, append([]string(nil), environment...))
	joined := strings.Join(args, " ")
	if runner.fail != "" && strings.Contains(joined, runner.fail) {
		return nil, errors.New("runner failed")
	}
	switch {
	case strings.Contains(joined, "image inspect"):
		return []byte("sha256:" + strings.Repeat("a", 64) + "\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "Config.Labels"):
		imageID := runner.inspectImageID
		if imageID == "" {
			imageID = "sha256:" + strings.Repeat("a", 64)
		}
		return []byte("tb_11111111111111111111111111111111|" + imageID + "\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "json .Mounts"):
		return []byte(`[{"Type":"bind","Source":"` + runner.workspace + `","Destination":"/workspace","RW":true},{"Type":"bind","Source":"` + runner.socket + `","Destination":"/run/mcp-devbox/container.sock","RW":true}]`), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "HostConfig.Memory"):
		return []byte("8589934592|4000000000|2048\n"), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "json .Config.Env"):
		return []byte(`["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","DOCKER_HOST=unix:///run/mcp-devbox/container.sock","CONTAINER_HOST=unix:///run/mcp-devbox/container.sock","MCP_DEVBOX_CONTAINER_ENGINE=podman","MCP_DEVBOX_CONTAINER_LABEL=mcp.devbox.toolbox.parent=tb_11111111111111111111111111111111","COMPOSE_PROJECT_NAME=mcp-tb-1111111111111111"]`), nil
	case strings.Contains(joined, " inspect ") && strings.Contains(joined, "SizeRw"):
		return []byte("4096|83886080\n"), nil
	case strings.Contains(joined, " inspect "):
		if runner.state == "" {
			runner.state = "running|true"
		}
		return []byte(runner.state + "\n"), nil
	case strings.Contains(joined, " create "):
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
	if reused || created.ToolboxID != "tb_11111111111111111111111111111111" || created.State != ProjectToolboxRunning || created.BaseImageID != "sha256:"+strings.Repeat("a", 64) || created.CPUMillis != 4000 || created.MemoryMiB != 8192 || created.ProcessLimit != 2048 {
		t.Fatalf("created=%+v reused=%v", created, reused)
	}
	var createCall string
	for _, call := range runner.calls {
		if strings.Contains(" "+strings.Join(call, " ")+" ", " create ") {
			createCall = strings.Join(call, " ")
		}
	}
	if !containsToolboxArgs(createCall, "--cpus 4.000", "--memory 8192m", "--pids-limit 2048", "--volume "+filepath.Join(stateRoot, "podman.sock")+":/run/mcp-devbox/container.sock:rw", "--env DOCKER_HOST=unix:///run/mcp-devbox/container.sock", "--env CONTAINER_HOST=unix:///run/mcp-devbox/container.sock", "--env MCP_DEVBOX_CONTAINER_ENGINE=podman", "--env MCP_DEVBOX_CONTAINER_LABEL=mcp.devbox.toolbox.parent=tb_11111111111111111111111111111111", "--env COMPOSE_PROJECT_NAME=mcp-tb-1111111111111111") {
		t.Fatalf("create call=%q", createCall)
	}
	if !created.ContainerAccess || created.WritableBytes != 4096 || created.RootFSBytes != 80<<20 {
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
			if !strings.Contains(joined, " --volume "+socketPath+":"+projectToolboxContainerSocket+":rw ") || strings.Contains(joined, "/var/run/docker.sock") {
				t.Fatalf("create authority=%q", call)
			}
		}
	}
	if !pullSeen || !createSeen {
		t.Fatalf("pull/create not observed: %v", runner.calls)
	}
}

func TestProjectToolboxRejectsContainerEndpointEnvironmentOverrides(t *testing.T) {
	manager, runner, workspace := testBrowserHarnessManager(t)
	for _, key := range []string{"CONTAINER_HOST", "DOCKER_HOST"} {
		value := "unix:///var/run/docker.sock"
		if _, err := manager.Exec(t.Context(), ProjectToolboxExecRequest{
			ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
			Argv: []string{"true"}, Environment: map[string]string{key: value},
		}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("exec override %s err=%v", key, err)
		}
		if _, _, err := manager.ServiceStart(t.Context(), ProjectToolboxServiceStartRequest{
			ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
			Name: "override-" + strings.ToLower(strings.TrimSuffix(key, "_HOST")), Argv: []string{"sleep", "1"}, Environment: map[string]string{key: value},
		}); !errors.Is(err, ErrProjectToolboxUnsafeState) {
			t.Fatalf("service override %s err=%v", key, err)
		}
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "/var/run/docker.sock") {
			t.Fatalf("rejected rootful endpoint reached runner: %q", call)
		}
	}
}
