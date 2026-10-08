//go:build opencode_e2e && !windows

package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestBubblewrapLinkedWorktreeGitMetadata(t *testing.T) {
	if os.Getenv("OPENCODE_BWRAP_HOST_E2E") != "1" {
		t.Skip("host Bubblewrap linked-worktree Git acceptance is explicit")
	}
	bubblewrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("Bubblewrap is required by the tagged linked-worktree Git acceptance")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("Git is required by the tagged linked-worktree Git acceptance")
	}
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
		Alias: "project", TargetAlias: "trusted-linux", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: fixture.head, Role: ProjectWorktreeWriter,
		JobID: "wj_dddddddddddddddddddddddddddddddd", LeaseID: "wl_dddddddddddddddddddddddddddddddd", Fence: 1,
		IdempotencyKey: "bubblewrap-linked-worktree-git",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.workspaces.Get(created.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := OpenOpenCodeRuntimeJournal(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.stateRoot, CodexPath: "/usr/bin/true", CodexPinPath: filepath.Join(fixture.stateRoot, "codex-pin.json"),
		BubblewrapPath: bubblewrapPath, OutputLimit: 4096, Workspaces: fixture.workspaces, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := ModelRuntimeLease{
		RuntimeID: "mr_dddddddddddddddddddddddddddddddd", DeviceID: "ed_dddddddddddddddddddddddddddddddd",
		WorkspaceID: workspace.ID, Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting,
		Goal: "commit the isolated fixture", GoalDigest: "sha256:" + strings.Repeat("d", 64), TimeoutSeconds: 60, ProviderProfile: remoteProviderProfile,
	}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.stateRoot, "r", lease.RuntimeID), workspace, LinuxWorkcellPreparation{Workspace: workspace}, "http://127.0.0.1:43210/v1", lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	separator := slices.Index(spec.Args, "--")
	if separator < 0 {
		t.Fatal("Bubblewrap command separator is missing")
	}
	runGit := func(arguments ...string) string {
		t.Helper()
		args := append(append([]string(nil), spec.Args[:separator+1]...), append([]string{gitPath}, arguments...)...)
		command := exec.Command(spec.Executable, args...)
		command.Dir, command.Env = spec.Dir, spec.Env
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("sandbox Git %v failed: %v: %s", arguments, runErr, boundedDiagnostic(string(output)))
		}
		return strings.TrimSpace(string(output))
	}
	if status := runGit("status", "--porcelain=v1"); status != "" {
		t.Fatalf("linked worktree is not initially clean: %q", status)
	}
	runGit("commit", "--allow-empty", "-m", "linked worktree acceptance")
	if status := runGit("status", "--porcelain=v1"); status != "" {
		t.Fatalf("linked worktree is not clean after commit: %q", status)
	}
	worktreeHead := strings.TrimSpace(runHostGit(t, created.path, "rev-parse", "HEAD"))
	canonicalHead := strings.TrimSpace(runHostGit(t, fixture.canonical.Path, "rev-parse", "HEAD"))
	if worktreeHead == fixture.head || canonicalHead != fixture.head {
		t.Fatalf("branch isolation failed: worktree=%s canonical=%s base=%s", worktreeHead, canonicalHead, fixture.head)
	}
}

func runHostGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("host Git %v failed: %v: %s", arguments, err, boundedDiagnostic(string(output)))
	}
	return string(output)
}

type bubblewrapIsolationReport struct {
	SchemaVersion                 int             `json:"schema_version"`
	Mode                          string          `json:"mode"`
	BubblewrapVersion             string          `json:"bubblewrap_version"`
	Runner                        string          `json:"runner"`
	GitTree                       string          `json:"git_tree"`
	OpenCodeVersion               string          `json:"opencode_version"`
	ProviderPackage               string          `json:"provider_package"`
	ProviderVersion               string          `json:"provider_version"`
	MountTargets                  []string        `json:"mount_targets"`
	PreflightStages               map[string]bool `json:"preflight_stages"`
	NetworkNamespaceUnshared      bool            `json:"network_namespace_unshared"`
	ExternalConnectBlocked        bool            `json:"external_connect_blocked"`
	ExternalDNSBlocked            bool            `json:"external_dns_blocked"`
	WorkspaceWritable             bool            `json:"workspace_writable"`
	RuntimeWritable               bool            `json:"runtime_writable"`
	ProviderReadOnly              bool            `json:"provider_read_only"`
	OpenCodeBinaryReadOnly        bool            `json:"opencode_binary_read_only"`
	EdgeStateVisible              bool            `json:"edge_state_visible"`
	HostHomeVisible               bool            `json:"host_home_visible"`
	RootVisible                   bool            `json:"root_visible"`
	SSHKeysVisible                bool            `json:"ssh_keys_visible"`
	BrowserProfilesVisible        bool            `json:"browser_profiles_visible"`
	VPNVisible                    bool            `json:"vpn_visible"`
	DockerSocketVisible           bool            `json:"docker_socket_visible"`
	WSLWindowsMountsVisible       bool            `json:"wsl_windows_mounts_visible"`
	UnixSocketReachable           bool            `json:"unix_socket_reachable"`
	TCPListenerFound              bool            `json:"tcp_listener_found"`
	UserNamespaceActive           bool            `json:"user_namespace_active"`
	RuntimeMode                   string          `json:"runtime_mode"`
	SocketMode                    string          `json:"socket_mode"`
	BubblewrapStartupMedianNanos  int64           `json:"bubblewrap_startup_median_nanos"`
	BubblewrapStartupSamplesNanos []int64         `json:"bubblewrap_startup_samples_nanos"`
}

type bubblewrapHelperReport struct {
	WorkspaceWritable       bool `json:"workspace_writable"`
	RuntimeWritable         bool `json:"runtime_writable"`
	ProviderReadOnly        bool `json:"provider_read_only"`
	OpenCodeBinaryReadOnly  bool `json:"opencode_binary_read_only"`
	EdgeStateVisible        bool `json:"edge_state_visible"`
	HostHomeVisible         bool `json:"host_home_visible"`
	RootVisible             bool `json:"root_visible"`
	SSHKeysVisible          bool `json:"ssh_keys_visible"`
	BrowserProfilesVisible  bool `json:"browser_profiles_visible"`
	VPNVisible              bool `json:"vpn_visible"`
	DockerSocketVisible     bool `json:"docker_socket_visible"`
	WSLWindowsMountsVisible bool `json:"wsl_windows_mounts_visible"`
	ExternalConnectBlocked  bool `json:"external_connect_blocked"`
	ExternalDNSBlocked      bool `json:"external_dns_blocked"`
	UnixSocketReachable     bool `json:"unix_socket_reachable"`
	TCPListenerFound        bool `json:"tcp_listener_found"`
	UserNamespaceActive     bool `json:"user_namespace_active"`
}

func TestBubblewrapRealIsolationSmoke(t *testing.T) {
	if os.Getenv("OPENCODE_BWRAP_HOST_E2E") != "1" {
		t.Skip("host Bubblewrap isolation is explicit")
	}
	bubblewrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("Bubblewrap is required by the tagged OpenCode E2E")
	}
	bubblewrapPath, err = filepath.Abs(bubblewrapPath)
	if err != nil {
		t.Fatal(err)
	}
	versionOutput, err := exec.Command(bubblewrapPath, "--version").Output()
	if err != nil {
		t.Fatalf("Bubblewrap version check failed: %v", err)
	}
	bubblewrapVersion := strings.TrimSpace(string(versionOutput))
	if bubblewrapVersion == "" || len(bubblewrapVersion) > 128 {
		t.Fatalf("unexpected Bubblewrap version output: %q", bubblewrapVersion)
	}

	root := t.TempDir()
	stateRoot := filepath.Join(root, "edge-state")
	runtimeDir := filepath.Join(root, "runtime")
	workspace := filepath.Join(root, "workspace")
	provider := filepath.Join(root, "provider")
	hostHome := filepath.Join(root, "host-home")
	for _, path := range []string{stateRoot, runtimeDir, workspace, provider, hostHome} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stateSentinel := filepath.Join(stateRoot, "identity-sentinel")
	homeSentinel := filepath.Join(hostHome, "home-sentinel")
	for _, path := range []string{stateSentinel, homeSentinel} {
		if err := os.WriteFile(path, []byte("not-visible\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := json.Marshal(map[string]any{
		"name": OpenCodeExternalDriverPackage, "version": "0.1.0", "private": true,
		"type": "module", "exports": "./index.js",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provider, "package.json"), append(manifest, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provider, "index.js"), []byte("export default {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper, err = filepath.EvalSymlinks(helper)
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(runtimeDir, openCodeDriverSocketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	socketDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			socketDone <- acceptErr
			return
		}
		defer connection.Close()
		buffer := make([]byte, 5)
		if _, readErr := io.ReadFull(connection, buffer); readErr != nil {
			socketDone <- readErr
			return
		}
		if string(buffer) != "ping\n" {
			socketDone <- errors.New("unexpected sandbox socket payload")
			return
		}
		_, writeErr := connection.Write([]byte("pong\n"))
		socketDone <- writeErr
	}()

	smokeLease := ModelRuntimeLease{RuntimeID: "smoke", Goal: "smoke", TimeoutSeconds: 30}
	configJSON := bubblewrapSmokeConfig(t, smokeLease)
	args, err := openCodeBubblewrapArgs(helper, provider, runtimeDir, workspace, smokeLease, configJSON, openCodeDefaultToolPath)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := parseOpenCodeSandboxArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOpenCodeSandboxSpec(sandbox, stateRoot, runtimeDir, workspace, provider, helper, openCodeDefaultToolPath, smokeLease); err != nil {
		t.Fatal(err)
	}
	separator := indexArgument(args, "--")
	if separator < 0 {
		t.Fatal("Bubblewrap separator missing")
	}
	smokeArgs := append([]string(nil), args[:separator]...)
	smokeArgs = append(smokeArgs,
		"--setenv", "MCP_DEVBOX_BWRAP_SMOKE", "1",
		"--setenv", "MCP_DEVBOX_STATE_SENTINEL", stateSentinel,
		"--setenv", "MCP_DEVBOX_HOME_SENTINEL", homeSentinel,
		"--", openCodeSandboxExecutable,
		"-test.run=^TestBubblewrapSandboxHelper$", "-test.count=1",
	)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bubblewrapPath, smokeArgs...)
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=" + openCodeDefaultToolPath, "HOME=" + hostHome, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	if err := cmd.Run(); err != nil {
		diagnostic := classifyBubblewrapFailure(bubblewrapStageHelperExec, err, stderr.String(), time.Since(started))
		t.Fatalf("slice_code=%s stage=%d exit_code=%d timed_out=%t duration_nanos=%d", diagnostic.Code, diagnostic.Stage, diagnostic.ExitCode, diagnostic.TimedOut, diagnostic.DurationNanos)
	}
	select {
	case socketErr := <-socketDone:
		if socketErr != nil {
			t.Fatal(socketErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sandbox did not reach the private Unix socket")
	}
	reportBytes, err := os.ReadFile(filepath.Join(runtimeDir, "smoke-report.json"))
	if err != nil {
		t.Fatalf("Bubblewrap helper did not write its report: %v, stdout=%s, stderr=%s", err, boundedDiagnostic(stdout.String()), boundedDiagnostic(stderr.String()))
	}
	var helperReport bubblewrapHelperReport
	if err := json.Unmarshal(reportBytes, &helperReport); err != nil {
		t.Fatalf("invalid Bubblewrap smoke report: %v", err)
	}
	if !helperReport.WorkspaceWritable || !helperReport.RuntimeWritable || !helperReport.ProviderReadOnly || !helperReport.OpenCodeBinaryReadOnly || helperReport.EdgeStateVisible || helperReport.HostHomeVisible || helperReport.RootVisible || helperReport.SSHKeysVisible || helperReport.BrowserProfilesVisible || helperReport.VPNVisible || helperReport.DockerSocketVisible || helperReport.WSLWindowsMountsVisible || !helperReport.ExternalConnectBlocked || !helperReport.ExternalDNSBlocked || !helperReport.UnixSocketReachable || helperReport.TCPListenerFound || !helperReport.UserNamespaceActive {
		t.Fatalf("Bubblewrap isolation invariants failed: %+v", helperReport)
	}

	preflight := readBubblewrapPreflightReport(t)
	startupSamples := measureBubblewrapStartup(t, bubblewrapPath, args, separator, workspace, hostHome)
	sortedSamples := append([]int64(nil), startupSamples...)
	sort.Slice(sortedSamples, func(left, right int) bool { return sortedSamples[left] < sortedSamples[right] })
	report := bubblewrapIsolationReport{
		SchemaVersion: 1, Mode: "bubblewrap_host_e2e", BubblewrapVersion: bubblewrapVersion, OpenCodeVersion: PinnedOpenCodeVersion,
		Runner: safeRunnerName(os.Getenv("P11_2_RUNNER")), GitTree: safeGitIdentity(os.Getenv("P11_2_GIT_TREE")),
		ProviderPackage: OpenCodeExternalDriverPackage, ProviderVersion: "0.1.0", MountTargets: sandboxMountTargets(sandbox),
		PreflightStages:          preflight.Stages,
		NetworkNamespaceUnshared: sandbox.UnshareAll,
		ExternalConnectBlocked:   helperReport.ExternalConnectBlocked, ExternalDNSBlocked: helperReport.ExternalDNSBlocked,
		WorkspaceWritable: helperReport.WorkspaceWritable, RuntimeWritable: helperReport.RuntimeWritable,
		ProviderReadOnly: helperReport.ProviderReadOnly, OpenCodeBinaryReadOnly: helperReport.OpenCodeBinaryReadOnly,
		EdgeStateVisible: helperReport.EdgeStateVisible, HostHomeVisible: helperReport.HostHomeVisible,
		RootVisible: helperReport.RootVisible, SSHKeysVisible: helperReport.SSHKeysVisible,
		BrowserProfilesVisible: helperReport.BrowserProfilesVisible, VPNVisible: helperReport.VPNVisible,
		DockerSocketVisible:     helperReport.DockerSocketVisible,
		WSLWindowsMountsVisible: helperReport.WSLWindowsMountsVisible, UnixSocketReachable: helperReport.UnixSocketReachable,
		TCPListenerFound:    helperReport.TCPListenerFound,
		UserNamespaceActive: helperReport.UserNamespaceActive, RuntimeMode: "0700", SocketMode: "0600",
		BubblewrapStartupMedianNanos: sortedSamples[len(sortedSamples)/2], BubblewrapStartupSamplesNanos: startupSamples,
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{root, stateSentinel, homeSentinel, "not-visible", "tool_arguments", "prompt", "token", "cookie", "authorization"} {
		if bytes.Contains(bytes.ToLower(encoded), bytes.ToLower([]byte(forbidden))) {
			t.Fatalf("isolation report leaked forbidden data marker %q", forbidden)
		}
	}
	artifactDir := filepath.Join(repoRootForBubblewrapSmoke(t), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "opencode-bubblewrap-isolation-report.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("BUBBLEWRAP_ISOLATION_REPORT_BYTES=%d", len(encoded))
}

func measureBubblewrapStartup(t *testing.T, bubblewrapPath string, args []string, separator int, workspace, hostHome string) []int64 {
	t.Helper()
	startupArgs := append([]string(nil), args[:separator+1]...)
	startupArgs = append(startupArgs, openCodeSandboxExecutable, "-test.run=^$", "-test.count=1")
	samples := make([]int64, 5)
	for index := range samples {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		started := time.Now()
		cmd := exec.CommandContext(ctx, bubblewrapPath, startupArgs...)
		cmd.Dir = workspace
		cmd.Env = []string{"PATH=" + openCodeDefaultToolPath, "HOME=" + hostHome, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
		cmd.Stdout = bytes.NewBuffer(nil)
		cmd.Stderr = bytes.NewBuffer(nil)
		err := cmd.Run()
		samples[index] = time.Since(started).Nanoseconds()
		cancel()
		if err != nil {
			t.Fatalf("Bubblewrap startup sample %d failed: %v", index, err)
		}
	}
	return samples
}

func sandboxMountTargets(spec openCodeSandboxSpec) []string {
	targets := make([]string, 0, len(spec.Mounts))
	for _, mount := range spec.Mounts {
		targets = append(targets, mount.Target)
	}
	sort.Strings(targets)
	return targets
}

func indexArgument(args []string, target string) int {
	for index, value := range args {
		if value == target {
			return index
		}
	}
	return -1
}

func boundedDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return value[len(value)-512:]
	}
	return value
}

func TestBubblewrapSandboxHelper(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_BWRAP_SMOKE") != "1" {
		t.Skip("Bubblewrap helper runs only inside the isolation smoke")
	}
	connection, err := net.DialTimeout("unix", openCodeSandboxSocket, 3*time.Second)
	socketOK := false
	if err == nil {
		_, _ = connection.Write([]byte("ping\n"))
		buffer := make([]byte, 5)
		_, readErr := io.ReadFull(connection, buffer)
		socketOK = readErr == nil && string(buffer) == "pong\n"
		_ = connection.Close()
	}
	connect, connectErr := net.DialTimeout("tcp", "1.1.1.1:443", 750*time.Millisecond)
	if connectErr == nil {
		_ = connect.Close()
	}
	dnsCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
	defer cancel()
	_, dnsErr := net.DefaultResolver.LookupHost(dnsCtx, "example.com")
	uidMap, uidErr := os.ReadFile("/proc/self/uid_map")
	report := bubblewrapHelperReport{
		WorkspaceWritable:       canCreateBubblewrapFile("/workspace/.bubblewrap-write-test"),
		RuntimeWritable:         canCreateBubblewrapFile("/runtime/.bubblewrap-write-test"),
		ProviderReadOnly:        !canCreateBubblewrapFile("/mcp-provider/.bubblewrap-write-test"),
		OpenCodeBinaryReadOnly:  bubblewrapFileReadOnly(openCodeSandboxExecutable),
		EdgeStateVisible:        bubblewrapPathExists(os.Getenv("MCP_DEVBOX_STATE_SENTINEL")),
		HostHomeVisible:         bubblewrapPathExists(os.Getenv("MCP_DEVBOX_HOME_SENTINEL")),
		RootVisible:             bubblewrapPathExists("/root"),
		SSHKeysVisible:          bubblewrapPathExists("/root/.ssh") || bubblewrapPathExists("/home/mcpedge/.ssh"),
		BrowserProfilesVisible:  bubblewrapPathExists("/root/.config/google-chrome") || bubblewrapPathExists("/home/mcpedge/.config/chromium") || bubblewrapPathExists("/mnt/c/Users"),
		VPNVisible:              bubblewrapPathExists("/etc/openvpn") || bubblewrapPathExists("/run/openvpn") || bubblewrapPathExists("/var/run/openvpn"),
		DockerSocketVisible:     bubblewrapPathExists("/run/docker.sock") || bubblewrapPathExists("/var/run/docker.sock"),
		WSLWindowsMountsVisible: bubblewrapPathExists("/mnt/c") || bubblewrapPathExists("/mnt/d"),
		ExternalConnectBlocked:  connectErr != nil,
		ExternalDNSBlocked:      dnsErr != nil,
		UnixSocketReachable:     socketOK,
		TCPListenerFound:        bubblewrapTCPListenerFound(),
		UserNamespaceActive:     uidErr == nil && len(strings.TrimSpace(string(uidMap))) > 0 && bubblewrapPathExists("/proc/self/ns/user"),
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/runtime/smoke-report.json", append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func bubblewrapSmokeConfig(t *testing.T, lease ModelRuntimeLease) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"provider": map[string]any{"bridge": map[string]any{
			"npm": "file:///mcp-provider", "name": "MCP Devbox External Driver",
			"options": map[string]any{"socketPath": openCodeSandboxSocket, "runtimeID": lease.RuntimeID, "ttlMs": int64(lease.TimeoutSeconds) * 1000, "timeoutMs": int64(lease.TimeoutSeconds) * 1000},
			"models":  map[string]any{"external-model": map[string]any{"name": "External Model Turn"}},
		}},
		"permission": map[string]any{"*": "allow", "external_directory": "deny", "webfetch": "deny", "websearch": "deny"},
		"agent":      map[string]any{"title": map[string]any{"disable": true}},
		"autoupdate": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func canCreateBubblewrapFile(path string) bool {
	err := os.WriteFile(path, []byte("ok"), 0o600)
	if err == nil {
		_ = os.Remove(path)
		return true
	}
	return false
}

func bubblewrapPathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

func repoRootForBubblewrapSmoke(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root not found")
		}
		directory = parent
	}
}
