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
	"sort"
	"strings"
	"testing"
	"time"
)

type bubblewrapIsolationReport struct {
	SchemaVersion                 int      `json:"schema_version"`
	BubblewrapVersion             string   `json:"bubblewrap_version"`
	OpenCodeVersion               string   `json:"opencode_version"`
	ProviderPackage               string   `json:"provider_package"`
	MountTargets                  []string `json:"mount_targets"`
	NetworkNamespaceUnshared      bool     `json:"network_namespace_unshared"`
	ExternalConnectBlocked        bool     `json:"external_connect_blocked"`
	ExternalDNSBlocked            bool     `json:"external_dns_blocked"`
	WorkspaceWritable             bool     `json:"workspace_writable"`
	RuntimeWritable               bool     `json:"runtime_writable"`
	ProviderReadOnly              bool     `json:"provider_read_only"`
	EdgeStateVisible              bool     `json:"edge_state_visible"`
	HostHomeVisible               bool     `json:"host_home_visible"`
	DockerSocketVisible           bool     `json:"docker_socket_visible"`
	WSLWindowsMountsVisible       bool     `json:"wsl_windows_mounts_visible"`
	UnixSocketReachable           bool     `json:"unix_socket_reachable"`
	UserNamespaceActive           bool     `json:"user_namespace_active"`
	RuntimeMode                   string   `json:"runtime_mode"`
	SocketMode                    string   `json:"socket_mode"`
	BubblewrapStartupMedianNanos  int64    `json:"bubblewrap_startup_median_nanos"`
	BubblewrapStartupSamplesNanos []int64  `json:"bubblewrap_startup_samples_nanos"`
}

type bubblewrapHelperReport struct {
	WorkspaceWritable       bool `json:"workspace_writable"`
	RuntimeWritable         bool `json:"runtime_writable"`
	ProviderReadOnly        bool `json:"provider_read_only"`
	EdgeStateVisible        bool `json:"edge_state_visible"`
	HostHomeVisible         bool `json:"host_home_visible"`
	DockerSocketVisible     bool `json:"docker_socket_visible"`
	WSLWindowsMountsVisible bool `json:"wsl_windows_mounts_visible"`
	ExternalConnectBlocked  bool `json:"external_connect_blocked"`
	ExternalDNSBlocked      bool `json:"external_dns_blocked"`
	UnixSocketReachable     bool `json:"unix_socket_reachable"`
	UserNamespaceActive     bool `json:"user_namespace_active"`
}

func TestBubblewrapRealIsolationSmoke(t *testing.T) {
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
	if err := cmd.Run(); err != nil {
		t.Fatalf("real Bubblewrap smoke failed: %v, stderr=%s", err, boundedDiagnostic(stderr.String()))
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
	if !helperReport.WorkspaceWritable || !helperReport.RuntimeWritable || !helperReport.ProviderReadOnly || helperReport.EdgeStateVisible || helperReport.HostHomeVisible || helperReport.DockerSocketVisible || helperReport.WSLWindowsMountsVisible || !helperReport.ExternalConnectBlocked || !helperReport.ExternalDNSBlocked || !helperReport.UnixSocketReachable || !helperReport.UserNamespaceActive {
		t.Fatalf("Bubblewrap isolation invariants failed: %+v", helperReport)
	}

	startupSamples := measureBubblewrapStartup(t, bubblewrapPath, args, separator, workspace, hostHome)
	sortedSamples := append([]int64(nil), startupSamples...)
	sort.Slice(sortedSamples, func(left, right int) bool { return sortedSamples[left] < sortedSamples[right] })
	report := bubblewrapIsolationReport{
		SchemaVersion: 1, BubblewrapVersion: bubblewrapVersion, OpenCodeVersion: PinnedOpenCodeVersion,
		ProviderPackage: OpenCodeExternalDriverPackage, MountTargets: sandboxMountTargets(sandbox),
		NetworkNamespaceUnshared: sandbox.UnshareAll,
		ExternalConnectBlocked:   helperReport.ExternalConnectBlocked, ExternalDNSBlocked: helperReport.ExternalDNSBlocked,
		WorkspaceWritable: helperReport.WorkspaceWritable, RuntimeWritable: helperReport.RuntimeWritable,
		ProviderReadOnly: helperReport.ProviderReadOnly, EdgeStateVisible: helperReport.EdgeStateVisible,
		HostHomeVisible: helperReport.HostHomeVisible, DockerSocketVisible: helperReport.DockerSocketVisible,
		WSLWindowsMountsVisible: helperReport.WSLWindowsMountsVisible, UnixSocketReachable: helperReport.UnixSocketReachable,
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
		EdgeStateVisible:        bubblewrapPathExists(os.Getenv("MCP_DEVBOX_STATE_SENTINEL")),
		HostHomeVisible:         bubblewrapPathExists(os.Getenv("MCP_DEVBOX_HOME_SENTINEL")),
		DockerSocketVisible:     bubblewrapPathExists("/run/docker.sock") || bubblewrapPathExists("/var/run/docker.sock"),
		WSLWindowsMountsVisible: bubblewrapPathExists("/mnt/c") || bubblewrapPathExists("/mnt/d"),
		ExternalConnectBlocked:  connectErr != nil,
		ExternalDNSBlocked:      dnsErr != nil,
		UnixSocketReachable:     socketOK,
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
