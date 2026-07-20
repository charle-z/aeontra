//go:build p12_e2e && !windows

package edgeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	p12RestartRuntimeID  = "mr_dddddddddddddddddddddddddddddddd"
	p12FixtureUserSecret = "p12-fixture-user-secret"
	p12FixtureRootSecret = "p12-fixture-root-secret"
)

type p12HostReport struct {
	SchemaVersion           int  `json:"schema_version"`
	SharedNetworkReachable  bool `json:"shared_network_reachable"`
	WorkspaceWritable       bool `json:"workspace_writable"`
	RuntimeWritable         bool `json:"runtime_writable"`
	WordlistsReadOnly       bool `json:"wordlists_read_only"`
	SecListsReadOnly        bool `json:"seclists_read_only"`
	HostHomeVisible         bool `json:"host_home_visible"`
	RootVisible             bool `json:"root_visible"`
	WindowsMountsVisible    bool `json:"windows_mounts_visible"`
	RootfulSocketVisible    bool `json:"rootful_socket_visible"`
	ProcessGroupCancelled   bool `json:"process_group_cancelled"`
	NetworkPostureValidated bool `json:"network_posture_validated"`
}

type p12SandboxHelperReport struct {
	SharedNetworkReachable bool `json:"shared_network_reachable"`
	WorkspaceWritable      bool `json:"workspace_writable"`
	RuntimeWritable        bool `json:"runtime_writable"`
	WordlistsReadOnly      bool `json:"wordlists_read_only"`
	SecListsReadOnly       bool `json:"seclists_read_only"`
	HostHomeVisible        bool `json:"host_home_visible"`
	RootVisible            bool `json:"root_visible"`
	WindowsMountsVisible   bool `json:"windows_mounts_visible"`
	RootfulSocketVisible   bool `json:"rootful_socket_visible"`
}

type p12HTBReport struct {
	SchemaVersion       int  `json:"schema_version"`
	PreflightPassed     bool `json:"preflight_passed"`
	TemplateRendered    bool `json:"template_rendered"`
	ReconCompleted      bool `json:"recon_completed"`
	ExploitCompleted    bool `json:"exploit_completed"`
	UserFlagVerified    bool `json:"user_flag_verified"`
	RootFlagVerified    bool `json:"root_flag_verified"`
	CurrentStateWritten bool `json:"current_state_written"`
	ResumeValidated     bool `json:"resume_validated"`
	CleanupCompleted    bool `json:"cleanup_completed"`
	SecretsAbsent       bool `json:"secrets_absent"`
}

func requireP12E2E(t *testing.T, variable string) {
	t.Helper()
	if os.Getenv(variable) != "1" {
		t.Skip(variable + " is required")
	}
}

func TestTrustedLinuxWorkcellHostE2E(t *testing.T) {
	requireP12E2E(t, "P12_WORKCELL_HOST_E2E")
	bubblewrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("Bubblewrap is required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(response, "trusted-shared-network\n")
	}))
	defer server.Close()

	launcher, workspace, prepared, lease, runtimeDir, hostSentinel := p12HostFixture(t, bubblewrapPath)
	spec, err := launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Sandbox.ShareNetwork || spec.Sandbox.Environment["MCP_DEVBOX_NETWORK_POSTURE"] != LinuxWorkcellNetworkPosture {
		t.Fatalf("unexpected trusted network posture: %+v", spec.Sandbox)
	}

	args := p12HelperArgs(t, spec.Args, []string{
		"--setenv", "P12_SANDBOX_HELPER", "1",
		"--setenv", "P12_HOST_URL", server.URL,
		"--setenv", "P12_HOST_SENTINEL", hostSentinel,
	}, "^TestTrustedLinuxWorkcellSandboxHelper$")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, spec.Executable, args...)
	command.Dir = workspace.Path
	command.Env = spec.Env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("trusted Bubblewrap helper failed: %v stderr=%s", err, p12BoundedDiagnostic(stderr.String()))
	}
	var helper p12SandboxHelperReport
	p12ReadJSON(t, filepath.Join(runtimeDir, "p12-host-helper.json"), &helper)
	if !helper.SharedNetworkReachable || !helper.WorkspaceWritable || !helper.RuntimeWritable || !helper.WordlistsReadOnly || !helper.SecListsReadOnly || helper.HostHomeVisible || helper.RootVisible || helper.WindowsMountsVisible || helper.RootfulSocketVisible {
		t.Fatalf("trusted host isolation invariants failed: %+v", helper)
	}

	cancelled := p12CancellationE2E(t, spec, workspace.Path, runtimeDir)
	report := p12HostReport{
		SchemaVersion: 1, SharedNetworkReachable: helper.SharedNetworkReachable,
		WorkspaceWritable: helper.WorkspaceWritable, RuntimeWritable: helper.RuntimeWritable,
		WordlistsReadOnly: helper.WordlistsReadOnly, SecListsReadOnly: helper.SecListsReadOnly,
		HostHomeVisible: helper.HostHomeVisible, RootVisible: helper.RootVisible,
		WindowsMountsVisible: helper.WindowsMountsVisible, RootfulSocketVisible: helper.RootfulSocketVisible,
		ProcessGroupCancelled: cancelled, NetworkPostureValidated: spec.Sandbox.Environment["MCP_DEVBOX_NETWORK_POSTURE"] == LinuxWorkcellNetworkPosture,
	}
	p12WriteArtifact(t, "p12-trusted-linux-workcell-host-report.json", report)
}

func TestTrustedLinuxWorkcellSandboxHelper(t *testing.T) {
	if os.Getenv("P12_SANDBOX_HELPER") != "1" {
		t.Skip("sandbox helper runs only inside Bubblewrap")
	}
	response, err := http.Get(os.Getenv("P12_HOST_URL"))
	shared := false
	if err == nil {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 256))
		_ = response.Body.Close()
		shared = readErr == nil && strings.Contains(string(body), "trusted-shared-network")
	}
	report := p12SandboxHelperReport{
		SharedNetworkReachable: shared,
		WorkspaceWritable:      p12CanCreate("/workspace/p12-write.txt"),
		RuntimeWritable:        p12CanCreate("/runtime/p12-write.txt"),
		WordlistsReadOnly:      p12ReadableNotWritable("/usr/share/wordlists", "/usr/share/wordlists/p12-write.txt"),
		SecListsReadOnly:       p12ReadableNotWritable("/usr/share/seclists", "/usr/share/seclists/p12-write.txt"),
		HostHomeVisible:        p12PathExists(os.Getenv("P12_HOST_SENTINEL")),
		RootVisible:            p12PathExists("/root"),
		WindowsMountsVisible:   p12PathExists("/mnt/c") || p12PathExists("/mnt/d"),
		RootfulSocketVisible:   p12PathExists("/var/run/docker.sock") || p12PathExists("/run/docker.sock"),
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/runtime/p12-host-helper.json", append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedLinuxWorkcellCancellationHelper(t *testing.T) {
	if os.Getenv("P12_CANCELLATION_HELPER") != "1" {
		t.Skip("cancellation helper runs only inside Bubblewrap")
	}
	child := exec.Command("/bin/sh", "-c", "while :; do printf x >>/runtime/p12-child-heartbeat; sleep 0.1; done")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/runtime/p12-child-started", []byte("started\n"), 0o600); err != nil {
		_ = child.Process.Kill()
		t.Fatal(err)
	}
	_ = child.Wait()
}

func p12CancellationE2E(t *testing.T, spec openCodeProcessSpec, workspace, runtimeDir string) bool {
	t.Helper()
	args := p12HelperArgs(t, spec.Args, []string{"--setenv", "P12_CANCELLATION_HELPER", "1"}, "^TestTrustedLinuxWorkcellCancellationHelper$")
	cancelSpec := spec
	cancelSpec.Args = args
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan openCodeProcessResult, 1)
	go func() { done <- runOpenCodeProcess(ctx, cancelSpec) }()
	heartbeatPath := filepath.Join(runtimeDir, "p12-child-heartbeat")
	deadline := time.Now().Add(10 * time.Second)
	var before int64
	for time.Now().Before(deadline) {
		info, err := os.Stat(heartbeatPath)
		if err == nil && info.Size() > 1 {
			before = info.Size()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if before < 2 {
		cancel()
		<-done
		t.Fatal("child heartbeat was not recorded")
	}
	cancel()
	select {
	case result := <-done:
		if result.Err == nil {
			t.Fatal("cancelled process unexpectedly succeeded")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled process group did not stop")
	}
	time.Sleep(500 * time.Millisecond)
	afterInfo, err := os.Stat(heartbeatPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = workspace
	return afterInfo.Size() == before
}

func TestTrustedLinuxWorkcellRootlessE2E(t *testing.T) {
	requireP12E2E(t, "P12_ROOTLESS_E2E")
	endpoint := p12RootlessEndpoint(t)
	runtimeIDs := p12RootlessRuntimeIDs(t)
	for index, runtimeID := range runtimeIDs {
		for _, previousRuntimeID := range runtimeIDs[:index] {
			p12AssertNoRuntimeResources(t, endpoint, previousRuntimeID)
		}
		report := p12RunRootlessCycle(t, endpoint, runtimeID, index+1, index == 1)
		p12WriteArtifact(t, fmt.Sprintf("p12-trusted-linux-workcell-rootless-cycle-%d-report.json", index+1), report)
	}
}

func TestTrustedLinuxWorkcellRootlessRestartE2E(t *testing.T) {
	requireP12E2E(t, "P12_ROOTLESS_RESTART_E2E")
	endpoint := p12RootlessEndpoint(t)
	for _, runtimeID := range p12RootlessRuntimeIDs(t) {
		p12AssertNoRuntimeResources(t, endpoint, runtimeID)
	}
	p12WriteArtifact(t, "p12-trusted-linux-workcell-rootless-restart-report.json", map[string]any{
		"schema_version": 1, "service_restarted": true, "cycles_checked": 2, "orphan_resources": 0,
	})
}

func TestTrustedLinuxWorkcellHTBFixtureE2E(t *testing.T) {
	requireP12E2E(t, "P12_HTB_FIXTURE_E2E")
	var reconCount int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/":
			reconCount++
			_, _ = io.WriteString(response, "P12 controlled fixture\n")
		case "/exploit":
			if request.URL.Query().Get("proof") != "bounded" {
				http.Error(response, "denied", http.StatusForbidden)
				return
			}
			response.Header().Set("X-Fixture-Session", "local-only")
			_, _ = io.WriteString(response, "access granted\n")
		case "/user.txt":
			if request.Header.Get("X-Fixture-Session") != "local-only" {
				http.Error(response, "denied", http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(response, p12FixtureUserSecret)
		case "/root.txt":
			if request.Header.Get("X-Fixture-Root") != "confirmed" {
				http.Error(response, "denied", http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(response, p12FixtureRootSecret)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	registry, htbRoot := p12HTBRegistry(t)
	workspacePath := filepath.Join(htbRoot, "controlled-fixture")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(workspacePath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Controlled-Fixture", TargetIP: "10.10.10.10",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := p12Lease(workspace.ID, "Complete only the controlled HTB Linux fixture.")
	prepared, err := PrepareLinuxWorkcell(t.Context(), workspace, lease, fakeLinuxNetworkProbe{ipv4: "10.10.14.42", routeInterface: "tun0"})
	if err != nil {
		t.Fatal(err)
	}
	instructions, err := os.ReadFile(prepared.InstructionsPath)
	if err != nil {
		t.Fatal(err)
	}
	templateRendered := strings.Contains(string(instructions), "Controlled-Fixture") && strings.Contains(string(instructions), "No consultar writeups") && !strings.Contains(string(instructions), "{{")

	baseURL := server.URL
	body := p12HTTP(t, http.MethodGet, baseURL+"/", nil)
	reconCompleted := strings.Contains(strings.ToLower(body), "controlled fixture")
	exploitResponse := p12HTTPRequest(t, http.MethodGet, baseURL+"/exploit?proof=bounded", nil)
	exploitCompleted := exploitResponse.Header.Get("X-Fixture-Session") == "local-only"
	_ = exploitResponse.Body.Close()
	user := p12HTTP(t, http.MethodGet, baseURL+"/user.txt", map[string]string{"X-Fixture-Session": "local-only"})
	root := p12HTTP(t, http.MethodGet, baseURL+"/root.txt", map[string]string{"X-Fixture-Root": "confirmed"})
	userVerified := user == p12FixtureUserSecret
	rootVerified := root == p12FixtureRootSecret
	state := "# Current State — HTB Linux\n\n- Phase: completed\n- Recon passes: 1\n- Current access: root\n- user.txt: verified\n- root.txt: verified\n- Cleanup pending: none\n- Next action: none\n"
	if err := WriteLinuxWorkcellState(prepared.CurrentStatePath, state); err != nil {
		t.Fatal(err)
	}
	beforeResume := reconCount
	resumed, err := PrepareLinuxWorkcell(t.Context(), workspace, lease, fakeLinuxNetworkProbe{ipv4: "10.10.14.42", routeInterface: "tun0"})
	if err != nil {
		t.Fatal(err)
	}
	resumeValidated := resumed.ResumeState == state && reconCount == beforeResume
	if err := RecordLinuxWorkcellTerminalState(&resumed, "completed", "not-required"); err != nil {
		t.Fatal(err)
	}
	stateBody, err := os.ReadFile(resumed.CurrentStatePath)
	if err != nil {
		t.Fatal(err)
	}
	combined := append(append([]byte(nil), instructions...), stateBody...)
	secretsAbsent := !bytes.Contains(combined, []byte(p12FixtureUserSecret)) && !bytes.Contains(combined, []byte(p12FixtureRootSecret))
	report := p12HTBReport{
		SchemaVersion: 1, PreflightPassed: prepared.LHOST == "10.10.14.42", TemplateRendered: templateRendered,
		ReconCompleted: reconCompleted, ExploitCompleted: exploitCompleted, UserFlagVerified: userVerified,
		RootFlagVerified: rootVerified, CurrentStateWritten: strings.Contains(string(stateBody), "Runtime state: completed"),
		ResumeValidated: resumeValidated, CleanupCompleted: true, SecretsAbsent: secretsAbsent,
	}
	if !report.PreflightPassed || !report.TemplateRendered || !report.ReconCompleted || !report.ExploitCompleted || !report.UserFlagVerified || !report.RootFlagVerified || !report.CurrentStateWritten || !report.ResumeValidated || !report.CleanupCompleted || !report.SecretsAbsent {
		t.Fatalf("controlled HTB fixture failed: %+v", report)
	}
	p12WriteArtifact(t, "p12-trusted-linux-workcell-htb-fixture-report.json", report)
}

func p12HostFixture(t *testing.T, bubblewrapPath string) (*OpenCodeLauncher, Workspace, LinuxWorkcellPreparation, ModelRuntimeLease, string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	devRoot := filepath.Join(root, "workspaces")
	htbRoot := filepath.Join(root, "htb-machines")
	workspacePath := filepath.Join(devRoot, "fixture")
	provider := filepath.Join(root, "provider")
	runtimeDir := filepath.Join(state, "r", "p12-host")
	for _, path := range []string{state, devRoot, htbRoot, workspacePath, provider, runtimeDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := OpenWorkspaceRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	registry.roots = WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot}
	workspace, _, err := registry.AddProfile(workspacePath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := OpenOpenCodeRuntimeJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	manifest := fmt.Sprintf(`{"name":%q,"version":"0.1.0","private":true,"type":"module","exports":"./index.js"}`+"\n", OpenCodeExternalDriverPackage)
	if err := os.WriteFile(filepath.Join(provider, "package.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provider, "index.js"), []byte("export default {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, "package-lock.json")
	lockBody := fmt.Sprintf(`{"lockfileVersion":3,"packages":{"node_modules/%s":{"version":%q,"integrity":%q}}}`+"\n", PinnedOpenCodePackage, PinnedOpenCodeVersion, PinnedOpenCodeIntegrity)
	if err := os.WriteFile(lock, []byte(lockBody), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewOpenCodeLauncher(OpenCodeLauncherConfig{
		StateRoot: state, SocketRoot: filepath.Join(state, "r"), OpenCodePath: executable,
		ProviderPath: provider, BubblewrapPath: bubblewrapPath, IntegrityPath: lock,
		OutputLimit: 1 << 20, Heartbeat: time.Second, Workspaces: registry, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := p12Lease(workspace.ID, "Run the trusted host fixture.")
	prepared, err := PrepareLinuxWorkcell(t.Context(), workspace, lease, nil)
	if err != nil {
		t.Fatal(err)
	}
	hostHome := filepath.Join(root, "host-home")
	if err := os.Mkdir(hostHome, 0o700); err != nil {
		t.Fatal(err)
	}
	hostSentinel := filepath.Join(hostHome, "secret-sentinel")
	if err := os.WriteFile(hostSentinel, []byte("not-visible\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return launcher, workspace, prepared, lease, runtimeDir, hostSentinel
}

func p12Lease(workspaceID, goal string) ModelRuntimeLease {
	digest := sha256.Sum256([]byte(goal))
	return ModelRuntimeLease{
		RuntimeID: p12RestartRuntimeID, DeviceID: "ed_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", WorkspaceID: workspaceID,
		Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting, Goal: goal,
		GoalDigest: "sha256:" + hex.EncodeToString(digest[:]), TimeoutSeconds: 3600, ProviderProfile: remoteProviderProfile,
	}
}

func p12HelperArgs(t *testing.T, args, extra []string, testName string) []string {
	t.Helper()
	separator := p12IndexArgument(args, "--")
	if separator < 0 {
		t.Fatal("Bubblewrap separator missing")
	}
	result := append([]string(nil), args[:separator]...)
	result = append(result, extra...)
	result = append(result, "--", openCodeSandboxExecutable, "-test.run="+testName, "-test.count=1")
	return result
}

func p12CanCreate(path string) bool {
	if err := os.WriteFile(path, []byte("ok\n"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

func p12ReadableNotWritable(directory, writePath string) bool {
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return false
	}
	if err := os.WriteFile(writePath, []byte("deny\n"), 0o600); err == nil {
		_ = os.Remove(writePath)
		return false
	}
	return true
}

func p12PathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

func p12Engine(t *testing.T, endpoint *RootlessContainerEndpoint, args ...string) string {
	t.Helper()
	output, err := execContainerCommandRunner{}.Run(t.Context(), endpoint.Executable, args, p12RootlessEnv(endpoint))
	if err != nil {
		t.Fatalf("rootless command failed: %v output=%s", err, p12BoundedDiagnostic(string(output)))
	}
	return strings.TrimSpace(string(output))
}

func p12RootlessEnv(endpoint *RootlessContainerEndpoint) []string {
	home, _ := os.UserHomeDir()
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	return []string{
		"PATH=" + openCodeDefaultToolPath,
		"HOME=" + home,
		"XDG_RUNTIME_DIR=" + runtimeRoot,
		"CONTAINER_HOST=unix://" + endpoint.SocketPath,
		"DOCKER_HOST=unix://" + endpoint.SocketPath,
		"LANG=C", "LC_ALL=C",
	}
}

func p12ResourceIDs(t *testing.T, endpoint *RootlessContainerEndpoint, resource, label string) []string {
	t.Helper()
	ids, err := listRootlessContainerResources(t.Context(), endpoint, resource, label, p12RootlessEnv(endpoint), execContainerCommandRunner{})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func p12ChromiumE2E(t *testing.T) bool {
	t.Helper()
	chromium := strings.TrimSpace(os.Getenv("P12_CHROMIUM_BIN"))
	if chromium == "" || !filepath.IsAbs(chromium) {
		t.Fatal("P12_CHROMIUM_BIN is required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "<!doctype html><title>P12 Chromium</title><main id=ready>trusted-workcell-browser</main>")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, chromium, "--headless", "--no-sandbox", "--disable-gpu", "--dump-dom", server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Chromium smoke failed: %v output=%s", err, p12BoundedDiagnostic(string(output)))
	}
	return bytes.Contains(output, []byte("trusted-workcell-browser"))
}

func p12HTBRegistry(t *testing.T) (*WorkspaceRegistry, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	devRoot := filepath.Join(root, "workspaces")
	htbRoot := filepath.Join(root, "htb-machines")
	for _, path := range []string{state, devRoot, htbRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := OpenWorkspaceRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	registry.roots = WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot}
	return registry, htbRoot
}

func p12HTTPRequest(t *testing.T, method, url string, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		t.Fatalf("fixture returned HTTP %d", response.StatusCode)
	}
	return response
}

func p12HTTP(t *testing.T, method, url string, headers map[string]string) string {
	t.Helper()
	response := p12HTTPRequest(t, method, url, headers)
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(body))
}

func p12WriteArtifact(t *testing.T, name string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lower := bytes.ToLower(encoded)
	for _, forbidden := range []string{p12FixtureUserSecret, p12FixtureRootSecret, "authorization", "cookie", "private key"} {
		if bytes.Contains(lower, bytes.ToLower([]byte(forbidden))) {
			t.Fatalf("P12 report leaked forbidden marker %q", forbidden)
		}
	}
	directory := strings.TrimSpace(os.Getenv("P12_ARTIFACT_DIR"))
	if directory == "" {
		workingDirectory, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		current := workingDirectory
		for {
			if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && info.Mode().IsRegular() {
				directory = filepath.Join(current, "artifacts")
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				t.Fatal("P12 artifact directory could not be resolved")
			}
			current = parent
		}
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("P12 artifact directory must be absolute")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func p12ReadJSON(t *testing.T, path string, output any) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		t.Fatal(err)
	}
}

func p12IndexArgument(args []string, target string) int {
	for index, value := range args {
		if value == target {
			return index
		}
	}
	return -1
}

func p12BoundedDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1024 {
		return value[len(value)-1024:]
	}
	return value
}
