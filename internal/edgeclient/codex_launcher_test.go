//go:build !windows

package edgeclient

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestCodexLinuxWorkcellSpecUsesOnlySignedHarnessAndLoopbackAdapter(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: fixture.lease.WorkspaceID, Path: fixture.workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	prepared := LinuxWorkcellPreparation{Workspace: workspace}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.state, "r", fixture.lease.RuntimeID), workspace, prepared, "http://127.0.0.1:43210/v1", fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{codexSandboxExecutable, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "danger-full-access", "--cd", openCodeSandboxWorkspace}
	if !slices.Equal(spec.Sandbox.Command[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("command prefix=%v want=%v", spec.Sandbox.Command, wantPrefix)
	}
	joined := strings.Join(spec.Sandbox.Command, " ")
	for _, required := range []string{
		`model_provider="mcp-devbox"`,
		`model_providers.mcp-devbox.base_url="http://127.0.0.1:43210/v1"`,
		`model_providers.mcp-devbox.wire_api="responses"`,
		`model_providers.mcp-devbox.requires_openai_auth=false`,
		`web_search="disabled"`,
		`agents.enabled=false`,
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Codex command is missing %q: %s", required, joined)
		}
	}
	for key := range spec.Sandbox.Environment {
		if strings.Contains(strings.ToUpper(key), "OPENAI") || strings.Contains(strings.ToUpper(key), "API_KEY") {
			t.Fatalf("credential-shaped environment escaped into Codex: %s", key)
		}
	}
	for key, want := range map[string]string{
		"GIT_AUTHOR_NAME": "MCP Devbox Codex", "GIT_AUTHOR_EMAIL": "codex@mcp-devbox.invalid",
		"GIT_COMMITTER_NAME": "MCP Devbox Codex", "GIT_COMMITTER_EMAIL": "codex@mcp-devbox.invalid",
	} {
		if spec.Sandbox.Environment[key] != want {
			t.Fatalf("%s=%q want=%q", key, spec.Sandbox.Environment[key], want)
		}
	}
	if mount := findSandboxMount(spec.Sandbox.Mounts, codexSandboxExecutable); mount.Source != codexPath || mount.Writable {
		t.Fatalf("Codex executable mount=%+v", mount)
	}
}

func TestCodexLinuxWorkcellRootlessSocketMountedAfterRuntime(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: fixture.lease.WorkspaceID, Path: fixture.workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	prepared := LinuxWorkcellPreparation{Workspace: workspace, RootlessContainer: &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock"}}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.state, "r", fixture.lease.RuntimeID), workspace, prepared, "http://127.0.0.1:43210/v1", fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	runtimeIndex, socketIndex := -1, -1
	for index, mount := range spec.Sandbox.Mounts {
		switch mount.Target {
		case openCodeSandboxRuntime:
			runtimeIndex = index
		case rootlessContainerSocketTarget:
			socketIndex = index
		}
	}
	if runtimeIndex < 0 || socketIndex <= runtimeIndex {
		t.Fatalf("rootless socket mount must follow runtime mount: runtime=%d socket=%d", runtimeIndex, socketIndex)
	}
}

func TestCodexLinuxWorkcellRootlessSocketRealBubblewrap(t *testing.T) {
	if os.Getenv("CODEX_ROOTLESS_BWRAP_E2E") != "1" {
		t.Skip("real rootless Bubblewrap mount acceptance is explicit")
	}
	bubblewrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := DiscoverRootlessContainerEndpoint(os.Geteuid(), openCodeDefaultToolPath)
	if err != nil || endpoint == nil {
		t.Fatalf("validated rootless endpoint is required: %v", err)
	}
	fixture := newOpenCodeLauncherFixture(t)
	codexPath := "/usr/bin/true"
	if source := os.Getenv("CODEX_CONTAINER_CLIENTS_ROOT"); source != "" {
		runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
		socket := filepath.Join(runtimeRoot, "docker.sock")
		if err := validateRootlessContainerSocket(socket, runtimeRoot, os.Geteuid()); err != nil {
			t.Fatalf("rootless Docker socket is required: %v", err)
		}
		root := t.TempDir()
		codexPath = filepath.Join(root, "codex")
		if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, relative := range []string{"bin/docker", "config/cli-plugins/docker-buildx"} {
			destination := filepath.Join(root, "container-tools", filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open(filepath.Join(source, filepath.FromSlash(relative)))
			if err != nil {
				t.Fatal(err)
			}
			output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
			if err != nil {
				_ = input.Close()
				t.Fatal(err)
			}
			_, copyErr := io.Copy(output, input)
			closeErr := output.Close()
			_ = input.Close()
			if copyErr != nil || closeErr != nil {
				t.Fatalf("copy managed client: %v %v", copyErr, closeErr)
			}
		}
		endpoint = &RootlessContainerEndpoint{Engine: "docker", SocketPath: socket, Executable: filepath.Join(root, "container-tools", "bin", "docker")}
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: filepath.Join(fixture.state, "pin.json"),
		BubblewrapPath: bubblewrap, OutputLimit: 4096, Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CODEX_CONTAINER_CLIENTS_ROOT") != "" {
		selected, err := launcher.rootlessEndpoint(os.Geteuid(), openCodeDefaultToolPath)
		if err != nil || selected == nil || selected.Engine != "docker" || selected.Executable != endpoint.Executable {
			t.Fatalf("signed Docker CLI was not selected for the validated rootless daemon: endpoint=%+v err=%v", selected, err)
		}
	}
	workspace := Workspace{ID: fixture.lease.WorkspaceID, Path: fixture.workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.state, "r", fixture.lease.RuntimeID), workspace,
		LinuxWorkcellPreparation{Workspace: workspace, RootlessContainer: endpoint}, "http://127.0.0.1:43210/v1", fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	separator := slices.Index(spec.Args, "--")
	if separator < 0 {
		t.Fatal("Bubblewrap command separator is missing")
	}
	commands := [][]string{{"/usr/bin/test", "-S", rootlessContainerSocketTarget}}
	if os.Getenv("CODEX_CONTAINER_CLIENTS_ROOT") != "" {
		commands = append(commands,
			[]string{"/container-tools/bin/docker", "version", "--format", "{{.Server.Version}}"},
			[]string{"/container-tools/bin/docker", "info", "--format", "{{json .SecurityOptions}}"},
			[]string{"/container-tools/bin/docker", "buildx", "version"},
		)
	}
	if os.Getenv("CODEX_ROOTLESS_PRIVILEGED_E2E") == "1" {
		if os.Getenv("CODEX_CONTAINER_CLIENTS_ROOT") == "" {
			t.Fatal("privileged Docker acceptance requires staged signed container clients")
		}
		commands = append(commands, []string{"/container-tools/bin/docker", "run", "--rm", "--pull=never", "--privileged", "--label", rootlessRuntimeLabelKey + "=" + fixture.lease.RuntimeID, "hello-world:latest"})
	}
	for _, arguments := range commands {
		args := append(append([]string(nil), spec.Args[:separator+1]...), arguments...)
		commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		command := exec.CommandContext(commandContext, spec.Executable, args...)
		command.Dir, command.Env = spec.Dir, spec.Env
		output, runErr := command.CombinedOutput()
		cancel()
		if runErr != nil {
			t.Fatalf("rootless workcell command %v failed: %v: %s", arguments, runErr, strings.TrimSpace(string(output)))
		} else {
			t.Logf("rootless workcell command %v: %s", arguments, strings.TrimSpace(string(output)))
		}
	}
}

func TestCodexLinuxWorkcellMountsManagedContainerClientsReadOnly(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	root := t.TempDir()
	codexPath := filepath.Join(root, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	clients := filepath.Join(root, "container-tools")
	for _, relative := range []string{"bin/docker", "config/cli-plugins/docker-buildx"} {
		path := filepath.Join(clients, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: filepath.Join(root, "pin.json"),
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: fixture.lease.WorkspaceID, Path: fixture.workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.state, "r", fixture.lease.RuntimeID), workspace,
		LinuxWorkcellPreparation{Workspace: workspace}, "http://127.0.0.1:43210/v1", fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if mount := findSandboxMount(spec.Sandbox.Mounts, codexContainerToolsMount); mount.Source != clients || mount.Writable || mount.Kind != "bind" {
		t.Fatalf("managed container clients mount=%+v", mount)
	}
	if !strings.HasPrefix(spec.Sandbox.Environment["PATH"], codexContainerToolsMount+"/bin:") || spec.Sandbox.Environment["DOCKER_CONFIG"] != "/toolchain/docker" {
		t.Fatal("managed Docker CLI or Buildx plugin is not discoverable in Codex workcell")
	}
	if mount := findSandboxMount(spec.Sandbox.Mounts, "/toolchain/docker/cli-plugins/docker-buildx"); mount.Writable || mount.Kind != "bind" {
		t.Fatalf("Buildx plugin mount=%+v", mount)
	}
	if err := os.Remove(filepath.Join(clients, "config", "cli-plugins", "docker-buildx")); err != nil {
		t.Fatal(err)
	}
	if _, err := codexManagedContainerTools(codexPath); err == nil {
		t.Fatal("partial signed container clients were accepted")
	}
}

func TestCodexLinkedWorktreeSpecMountsExactGitMetadata(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	manager, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{
		StateRoot: fixture.stateRoot, Roots: fixture.roots, Workspaces: fixture.workspaces,
		Runner:     NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("c", 36)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	created, _, err := manager.Create(context.Background(), ProjectWorktreeCreateRequest{
		Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: fixture.head, Role: ProjectWorktreeWriter,
		JobID: "wj_cccccccccccccccccccccccccccccccc", LeaseID: "wl_cccccccccccccccccccccccccccccccc", Fence: 1,
		IdempotencyKey: "worktree-codex-git-metadata",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.workspaces.Get(created.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bubblewrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bubblewrap, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenOpenCodeRuntimeJournal(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.stateRoot, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: bubblewrap, OutputLimit: 4096, Workspaces: fixture.workspaces, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := ModelRuntimeLease{
		RuntimeID: "mr_cccccccccccccccccccccccccccccccc", DeviceID: "ed_cccccccccccccccccccccccccccccccc",
		WorkspaceID: workspace.ID, Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting,
		Goal: "commit the isolated fixture", GoalDigest: "sha256:" + strings.Repeat("c", 64), TimeoutSeconds: 60, ProviderProfile: remoteProviderProfile,
	}
	runtimeDir := filepath.Join(fixture.stateRoot, "r", lease.RuntimeID)
	spec, err := launcher.codexLinuxWorkcellProcessSpec(runtimeDir, workspace, LinuxWorkcellPreparation{Workspace: workspace}, "http://127.0.0.1:43210/v1", lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	gitPointer, err := os.ReadFile(filepath.Join(created.path, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Clean(strings.TrimSpace(strings.TrimPrefix(string(gitPointer), "gitdir: ")))
	for key, want := range map[string]string{
		"GIT_DIR": filepath.ToSlash(filepath.Join(codexSandboxGitCommon, "worktrees", filepath.Base(gitDir))), "GIT_WORK_TREE": openCodeSandboxWorkspace,
	} {
		if spec.Sandbox.Environment[key] != want {
			t.Fatalf("%s=%q want=%q", key, spec.Sandbox.Environment[key], want)
		}
	}
	if got := spec.Sandbox.Environment["GIT_COMMON_DIR"]; got != "" {
		t.Fatalf("GIT_COMMON_DIR=%q want empty so the linked gitdir resolves commondir itself", got)
	}
	mount := findSandboxMount(spec.Sandbox.Mounts, codexSandboxGitCommon)
	wantSource := filepath.Join(fixture.canonical.Path, ".git")
	if mount.Source != wantSource || !mount.Writable || mount.Kind != "bind" {
		t.Fatalf("Git common metadata mount=%+v want source=%s writable bind", mount, wantSource)
	}
}

func TestCodexLauncherCompletesOneDurableLinuxWorkcellLease(t *testing.T) {
	fixture, workspace, _, lease, _ := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nprintf 'codex-cli 0.147.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Heartbeat: time.Second,
		Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	launcher.allowRootTest = true
	launcher.config.RuntimeStartupBudget = 0
	launcher.rootlessEndpoint = nil
	launcher.verifyCodexInstallation = func(string, string) error { return nil }
	launcher.verifySandbox = func(context.Context, openCodeProcessSpec) error { return nil }
	fixture.remote.runtime.WorkspaceID = workspace.ID
	launcher.remoteFactory = func(ModelRuntimeLease) (OpenCodeRemoteTransport, error) { return fixture.remote, nil }
	var captured openCodeProcessSpec
	launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		captured = spec
		if spec.Started == nil {
			t.Fatal("Codex process start observer is missing")
		}
		if err := spec.Started(); err != nil {
			t.Fatal(err)
		}
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := launcher.RunLease(context.Background(), lease)
	if err != nil || result.State != OpenCodeLocalCompleted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if captured.Sandbox.Environment["CODEX_HOME"] != openCodeSandboxHome {
		t.Fatalf("Codex sandbox was not captured: %+v", captured.Sandbox)
	}
	wantPhases := []modelturn.RuntimePhase{modelturn.RuntimePhaseLocalPreflightComplete, modelturn.RuntimePhaseModelAdapterReady, modelturn.RuntimePhaseCodexProcessStarted}
	fixture.remote.mu.Lock()
	phases := append([]modelturn.RuntimePhase(nil), fixture.remote.phases...)
	fixture.remote.mu.Unlock()
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("phases=%v want=%v", phases, wantPhases)
	}
}

func findSandboxMount(mounts []openCodeSandboxMount, target string) openCodeSandboxMount {
	for _, mount := range mounts {
		if mount.Target == target {
			return mount
		}
	}
	return openCodeSandboxMount{}
}
