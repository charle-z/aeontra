//go:build opencode_e2e && !windows

package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type bubblewrapPreflightReport struct {
	SchemaVersion     int             `json:"schema_version"`
	Mode              string          `json:"mode"`
	BubblewrapVersion string          `json:"bubblewrap_version"`
	Runner            string          `json:"runner"`
	GitTree           string          `json:"git_tree"`
	Stages            map[string]bool `json:"stages"`
	DurationNanos     int64           `json:"duration_nanos"`
}

func TestBubblewrapHostPreflight(t *testing.T) {
	if os.Getenv("OPENCODE_BWRAP_HOST_E2E") != "1" {
		t.Skip("host Bubblewrap preflight is explicit")
	}
	started := time.Now()
	bubblewrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		diagnostic := classifyBubblewrapFailure(bubblewrapStageVersion, err, "not found", time.Since(started))
		t.Fatalf("slice_code=%s stage=%d exit_code=%d timed_out=%t duration_nanos=%d", diagnostic.Code, diagnostic.Stage, diagnostic.ExitCode, diagnostic.TimedOut, diagnostic.DurationNanos)
	}
	versionOutput, err := exec.Command(bubblewrapPath, "--version").Output()
	if err != nil {
		diagnostic := classifyBubblewrapFailure(bubblewrapStageVersion, err, "", time.Since(started))
		t.Fatalf("slice_code=%s stage=%d exit_code=%d timed_out=%t duration_nanos=%d", diagnostic.Code, diagnostic.Stage, diagnostic.ExitCode, diagnostic.TimedOut, diagnostic.DurationNanos)
	}
	version := strings.TrimSpace(string(versionOutput))

	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	runtimeDir := filepath.Join(root, "runtime")
	provider := filepath.Join(root, "provider")
	for _, directory := range []string{workspace, runtimeDir, provider} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal("preflight fixture creation failed")
		}
	}
	if err := os.WriteFile(filepath.Join(provider, "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal("preflight provider fixture failed")
	}
	socketPath := filepath.Join(runtimeDir, "preflight.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal("preflight socket creation failed")
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal("preflight socket mode failed")
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal("preflight helper resolution failed")
	}

	stages := make(map[string]bool, bubblewrapStageMax)
	for stage := bubblewrapStageUserNamespace; stage <= bubblewrapStageHelperExec; stage++ {
		args := bubblewrapPreflightArgs(stage, helper, workspace, runtimeDir, provider)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var stderr bytes.Buffer
		stageStarted := time.Now()
		cmd := exec.CommandContext(ctx, bubblewrapPath, args...)
		cmd.Stderr = &stderr
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
		err := cmd.Run()
		duration := time.Since(stageStarted)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		cancel()
		if err != nil {
			diagnostic := classifyBubblewrapFailure(stage, err, stderr.String(), duration)
			t.Fatalf("slice_code=%s stage=%d exit_code=%d timed_out=%t duration_nanos=%d", diagnostic.Code, diagnostic.Stage, diagnostic.ExitCode, diagnostic.TimedOut, diagnostic.DurationNanos)
		}
		stages[bubblewrapPreflightStageName(stage)] = true
	}

	report := bubblewrapPreflightReport{
		SchemaVersion:     1,
		Mode:              "bubblewrap_host_e2e",
		BubblewrapVersion: version,
		Runner:            safeRunnerName(os.Getenv("P11_2_RUNNER")),
		GitTree:           safeGitIdentity(os.Getenv("P11_2_GIT_TREE")),
		Stages:            stages,
		DurationNanos:     time.Since(started).Nanoseconds(),
	}
	writeBubblewrapPreflightReport(t, report)
}

func bubblewrapPreflightArgs(stage int, helper, workspace, runtimeDir, provider string) []string {
	args := []string{"--die-with-parent", "--new-session"}
	if stage >= bubblewrapStageUnshareAll {
		args = append(args, "--unshare-all")
	} else {
		args = append(args, "--unshare-user")
	}
	if stage >= bubblewrapStageUIDMap {
		args = append(args, "--uid", "0")
	}
	if stage >= bubblewrapStageGIDMap {
		args = append(args, "--gid", "0")
	}
	args = append(args, "--clearenv")
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	if stage >= bubblewrapStageProcMount {
		args = append(args, "--proc", "/proc")
	}
	if stage >= bubblewrapStageDevMount {
		args = append(args, "--dev", "/dev")
	}
	if stage >= bubblewrapStageTmpfsMount {
		args = append(args, "--tmpfs", "/tmp")
	}
	if stage >= bubblewrapStageWorkspaceBind {
		args = append(args, "--bind", workspace, "/workspace")
	}
	if stage >= bubblewrapStageRuntimeBind {
		args = append(args, "--bind", runtimeDir, "/runtime")
	}
	if stage >= bubblewrapStageProviderBind {
		args = append(args, "--ro-bind", provider, "/provider")
	}
	args = append(args, "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "LC_ALL", "C.UTF-8")

	command := []string{"/bin/true"}
	switch stage {
	case bubblewrapStageEmptyFilesystem:
		command = []string{"/bin/sh", "-eu", "-c", "test ! -e /host-sentinel"}
	case bubblewrapStageProcMount:
		command = []string{"/bin/sh", "-eu", "-c", "test -r /proc/self/status"}
	case bubblewrapStageDevMount:
		command = []string{"/bin/sh", "-eu", "-c", "test -c /dev/null"}
	case bubblewrapStageTmpfsMount:
		command = []string{"/bin/sh", "-eu", "-c", "printf ok >/tmp/preflight && test -s /tmp/preflight"}
	case bubblewrapStageSystemBind:
		command = []string{"/bin/sh", "-eu", "-c", "test -x /bin/sh"}
	case bubblewrapStageWorkspaceBind:
		command = []string{"/bin/sh", "-eu", "-c", "printf ok >/workspace/preflight && test -s /workspace/preflight"}
	case bubblewrapStageRuntimeBind:
		command = []string{"/bin/sh", "-eu", "-c", "printf ok >/runtime/preflight && test -s /runtime/preflight"}
	case bubblewrapStageProviderBind:
		command = []string{"/bin/sh", "-eu", "-c", "test -r /provider/package.json && ! touch /provider/blocked"}
	case bubblewrapStageUnixSocket:
		command = []string{"/bin/sh", "-eu", "-c", "test -S /runtime/preflight.sock"}
	case bubblewrapStageUnshareAll, bubblewrapStageHelperExec:
		args = append(args, "--ro-bind", helper, "/preflight-helper", "--setenv", "MCP_DEVBOX_BWRAP_PREFLIGHT", "1")
		command = []string{"/preflight-helper", "-test.run=TestBubblewrapPreflightHelper", "-test.count=1"}
	}
	return append(args, append([]string{"--"}, command...)...)
}

func TestBubblewrapPreflightHelper(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_BWRAP_PREFLIGHT") != "1" {
		t.Skip("preflight helper runs only inside Bubblewrap")
	}
	if _, err := os.Stat("/host-sentinel"); !os.IsNotExist(err) {
		t.Fatal("host filesystem became visible")
	}
	if _, err := net.DialTimeout("tcp", "1.1.1.1:443", 250*time.Millisecond); err == nil {
		t.Fatal("external connect was not blocked")
	}
	if _, err := net.DefaultResolver.LookupHost(context.Background(), "example.com"); err == nil {
		t.Fatal("external DNS was not blocked")
	}
}

func bubblewrapPreflightStageName(stage int) string {
	names := map[int]string{
		bubblewrapStageUserNamespace:   "user_namespace",
		bubblewrapStageUIDMap:          "uid_map",
		bubblewrapStageGIDMap:          "gid_map",
		bubblewrapStageEmptyFilesystem: "empty_filesystem",
		bubblewrapStageProcMount:       "proc_mount",
		bubblewrapStageDevMount:        "dev_mount",
		bubblewrapStageTmpfsMount:      "tmpfs_mount",
		bubblewrapStageSystemBind:      "system_bind_read_only",
		bubblewrapStageWorkspaceBind:   "workspace_bind_read_write",
		bubblewrapStageRuntimeBind:     "runtime_bind_read_write",
		bubblewrapStageProviderBind:    "provider_bind_read_only",
		bubblewrapStageUnixSocket:      "unix_socket",
		bubblewrapStageUnshareAll:      "unshare_all_network_blocked",
		bubblewrapStageHelperExec:      "helper_exec",
	}
	return names[stage]
}

func safeRunnerName(value string) string {
	switch value {
	case "ubuntu-22.04", "ubuntu-24.04", "parrot-wsl2":
		return value
	default:
		return "unknown"
	}
}

func safeGitIdentity(value string) string {
	if len(value) != 40 {
		return "unknown"
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "unknown"
		}
	}
	return value
}

func writeBubblewrapPreflightReport(t *testing.T, report bubblewrapPreflightReport) {
	t.Helper()
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal("preflight report encoding failed")
	}
	root := repoRootFromEdgeClientTest(t)
	artifactDir := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal("preflight artifact directory failed")
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "opencode-bubblewrap-preflight-report.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal("preflight report write failed")
	}
}

func repoRootFromEdgeClientTest(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal("repository root resolution failed")
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("repository root not found")
		}
		current = parent
	}
}
