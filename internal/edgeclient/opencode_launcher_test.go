//go:build !windows

package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type openCodeLauncherFixture struct {
	state      string
	workspace  string
	provider   string
	executable string
	bubblewrap string
	lock       string
	registry   *WorkspaceRegistry
	journal    *OpenCodeRuntimeJournal
	launcher   *OpenCodeLauncher
	lease      ModelRuntimeLease
	remote     *fakeOpenCodeRemote
}

type fakeOpenCodeRemote struct {
	mu             sync.Mutex
	runtime        modelturn.Runtime
	startedCalls   int
	heartbeatCalls int
	failedCalls    int
	completedCalls int
	heartbeatState modelturn.RuntimeState
	heartbeatErr   error
	phases         []modelturn.RuntimePhase
	retries        map[modelturn.RuntimeRetryCategory]uint32
}

func newOpenCodeLauncherFixture(t *testing.T) *openCodeLauncherFixture {
	t.Helper()
	state, err := os.MkdirTemp("", "mde-state-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(state) })
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry, err := OpenWorkspaceRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	registered, created, err := registry.Add(workspace)
	if err != nil || !created {
		t.Fatalf("register workspace: created=%t err=%v", created, err)
	}
	journal, err := OpenOpenCodeRuntimeJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })

	provider := filepath.Join(t.TempDir(), "provider")
	if err := os.Mkdir(provider, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":"0.1.0","private":true,"type":"module","exports":"./index.js"}`+"\n", OpenCodeExternalDriverPackage)
	if err := os.WriteFile(filepath.Join(provider, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provider, "index.js"), []byte("export default {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(t.TempDir(), "package-lock.json")
	lockBody := fmt.Sprintf(`{"lockfileVersion":3,"packages":{"node_modules/%s":{"version":%q,"integrity":%q}}}`+"\n", PinnedOpenCodePackage, PinnedOpenCodeVersion, PinnedOpenCodeIntegrity)
	if err := os.WriteFile(lockPath, []byte(lockBody), 0o644); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "opencode")
	writeOpenCodeVersionScript(t, executable, PinnedOpenCodeVersion, "")
	bubblewrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bubblewrap, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	socketRoot := filepath.Join(state, openCodeRuntimeDirName)
	launcher, err := NewOpenCodeLauncher(OpenCodeLauncherConfig{
		StateRoot: state, SocketRoot: socketRoot, OpenCodePath: executable, ProviderPath: provider, BubblewrapPath: bubblewrap, IntegrityPath: lockPath,
		OutputLimit: 4096, Heartbeat: time.Second, Workspaces: registry, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	launcher.allowRootTest = true
	launcher.verifySandbox = func(ctx context.Context, _ openCodeProcessSpec) error {
		output, err := exec.CommandContext(ctx, executable, "--version").Output()
		if err != nil {
			return errors.New("bubblewrap could not create the required OpenCode sandbox")
		}
		if strings.TrimSpace(string(output)) != PinnedOpenCodeVersion {
			return errors.New("OpenCode version does not match the pinned release")
		}
		return nil
	}
	goal := "Fix the bounded fixture and run its tests."
	sum := sha256.Sum256([]byte(goal))
	lease := ModelRuntimeLease{
		RuntimeID: "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DeviceID: "ed_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		WorkspaceID: registered.ID, Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting,
		Goal: goal, GoalDigest: "sha256:" + hex.EncodeToString(sum[:]), TimeoutSeconds: 5, ProviderProfile: remoteProviderProfile,
	}
	remote := &fakeOpenCodeRemote{runtime: modelturn.Runtime{
		RuntimeID: lease.RuntimeID, DeviceID: lease.DeviceID, WorkspaceID: lease.WorkspaceID,
		Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting, Status: modelturn.RuntimeRunning,
	}}
	launcher.remoteFactory = func(ModelRuntimeLease) (OpenCodeRemoteTransport, error) { return remote, nil }
	return &openCodeLauncherFixture{
		state: state, workspace: workspace, provider: provider, executable: executable, bubblewrap: bubblewrap, lock: lockPath,
		registry: registry, journal: journal, launcher: launcher, lease: lease, remote: remote,
	}
}

func writeOpenCodeVersionScript(t *testing.T, path, version, versionHook string) {
	t.Helper()
	body := "#!/bin/sh\nset -eu\nif [ \"${1:-}\" = \"--version\" ]; then\n" + versionHook + "\nprintf '%s\\n' '" + version + "'\nexit 0\nfi\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeOpenCodeRemote) CreateTurn(context.Context, modelturn.ModelRequest) (modelturn.Turn, error) {
	return modelturn.Turn{}, errors.New("unexpected model turn in launcher unit test")
}
func (f *fakeOpenCodeRemote) WaitResponse(context.Context, modelturn.TurnID) (modelturn.ModelResponse, error) {
	return modelturn.ModelResponse{}, errors.New("unexpected model wait in launcher unit test")
}
func (f *fakeOpenCodeRemote) Cancel(context.Context, modelturn.TurnID) error { return nil }
func (f *fakeOpenCodeRemote) ReportPhase(_ context.Context, phase modelturn.RuntimePhase, category modelturn.RuntimeRetryCategory, count uint32) (modelturn.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.phases = append(f.phases, phase)
	if category != "" {
		if f.retries == nil {
			f.retries = make(map[modelturn.RuntimeRetryCategory]uint32)
		}
		f.retries[category] += count
	}
	return f.runtime, nil
}
func (f *fakeOpenCodeRemote) Started(context.Context) (modelturn.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startedCalls++
	f.runtime.State = modelturn.RuntimeStateAwaitingModel
	return f.runtime, nil
}
func (f *fakeOpenCodeRemote) Heartbeat(context.Context) (modelturn.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeatCalls++
	if f.heartbeatErr != nil {
		return modelturn.Runtime{}, f.heartbeatErr
	}
	if f.heartbeatState != "" {
		f.runtime.State = f.heartbeatState
	}
	return f.runtime, nil
}
func (f *fakeOpenCodeRemote) Failed(context.Context, string) (modelturn.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedCalls++
	f.runtime.State = modelturn.RuntimeStateFailed
	f.runtime.Status = modelturn.RuntimeFailed
	return f.runtime, nil
}
func (f *fakeOpenCodeRemote) Completed(context.Context, string) (modelturn.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completedCalls++
	f.runtime.State = modelturn.RuntimeStateCompleted
	f.runtime.Status = modelturn.RuntimeCompleted
	return f.runtime, nil
}
func (f *fakeOpenCodeRemote) Close() error { return nil }

func TestOpenCodeLauncherUsesOnlyFixedLocalConfiguration(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	fixture.lease.RetryCounts = map[modelturn.RuntimeRetryCategory]uint32{modelturn.RuntimeRetryGatewayTimeout: 2}
	var captured openCodeProcessSpec
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		captured = spec
		if spec.Started == nil {
			t.Fatal("process start observer is missing")
		}
		if err := spec.Started(); err != nil {
			t.Fatal(err)
		}
		socketPath := filepath.Join(openCodeRuntimeDir(fixture.launcher.config.SocketRoot, fixture.lease.RuntimeID), openCodeDriverSocketName)
		info, err := os.Lstat(socketPath)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
			t.Fatalf("socket mode=%v err=%v", infoMode(info), err)
		}
		parent, err := os.Lstat(filepath.Dir(socketPath))
		if err != nil || parent.Mode().Perm() != 0o700 {
			t.Fatalf("socket dir mode=%v err=%v", infoMode(parent), err)
		}
		_, _ = spec.Stdout.Write([]byte("bounded stdout"))
		_, _ = spec.Stderr.Write([]byte("bounded stderr"))
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	if err != nil || result.State != OpenCodeLocalCompleted || result.OutputTruncated {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if captured.Dir != fixture.workspace || captured.Executable != fixture.bubblewrap {
		t.Fatalf("process spec=%+v", captured)
	}
	reparsed, err := parseOpenCodeSandboxArgs(captured.Args)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reparsed, captured.Sandbox) {
		t.Fatalf("parsed sandbox differs from executed sandbox: parsed=%+v captured=%+v", reparsed, captured.Sandbox)
	}
	runtimeDir := openCodeRuntimeDir(fixture.launcher.config.SocketRoot, fixture.lease.RuntimeID)
	resolvedOpenCode, err := filepath.EvalSymlinks(fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	expectedMounts := make([]openCodeSandboxMount, 0)
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc/ssl/certs", "/etc/ca-certificates"} {
		if info, statErr := os.Stat(systemPath); statErr == nil && info.IsDir() {
			expectedMounts = append(expectedMounts, openCodeSandboxMount{Source: systemPath, Target: systemPath, Kind: "bind"})
		}
	}
	expectedMounts = append(expectedMounts,
		openCodeSandboxMount{Target: "/proc", Writable: true, Kind: "proc"},
		openCodeSandboxMount{Target: "/dev", Writable: true, Kind: "dev"},
		openCodeSandboxMount{Target: "/tmp", Writable: true, Kind: "tmpfs"},
		openCodeSandboxMount{Source: resolvedOpenCode, Target: openCodeSandboxExecutable, Kind: "bind"},
		openCodeSandboxMount{Source: fixture.provider, Target: openCodeSandboxProvider, Kind: "bind"},
		openCodeSandboxMount{Source: runtimeDir, Target: openCodeSandboxRuntime, Writable: true, Kind: "bind"},
		openCodeSandboxMount{Source: fixture.workspace, Target: openCodeSandboxWorkspace, Writable: true, Kind: "bind"},
	)
	expectedEnv := map[string]string{
		"PATH": openCodeDefaultToolPath, "HOME": openCodeSandboxHome, "USER": "mcpedge",
		"LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TERM": "dumb", "SHELL": "/bin/sh",
		"XDG_CONFIG_HOME":                 openCodeSandboxHome + "/.config",
		"XDG_DATA_HOME":                   openCodeSandboxHome + "/.local/share",
		"XDG_STATE_HOME":                  openCodeSandboxHome + "/.local/state",
		"XDG_CACHE_HOME":                  openCodeSandboxHome + "/.cache",
		"OPENCODE_TEST_HOME":              openCodeSandboxHome,
		"OPENCODE_CONFIG_CONTENT":         captured.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"],
		"OPENCODE_AUTH_CONTENT":           "{}",
		"OPENCODE_DISABLE_PROJECT_CONFIG": "1", "OPENCODE_PURE": "1",
		"OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_DISABLE_AUTOCOMPACT": "1",
		"OPENCODE_DISABLE_MODELS_FETCH": "1", "OPENCODE_DISABLE_LSP_DOWNLOAD": "1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS": "1", "OPENCODE_DISABLE_EXTERNAL_SKILLS": "1",
		"OPENCODE_DISABLE_SHARE": "1",
	}
	expected := openCodeSandboxSpec{
		UnshareAll: true, ClearEnv: true, NewSession: true, DieWithParent: true,
		Mounts: expectedMounts, Environment: expectedEnv, WorkingDirectory: openCodeSandboxWorkspace,
		Command: []string{openCodeSandboxExecutable, "run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", openCodeSandboxWorkspace, fixture.lease.Goal},
	}
	if !reflect.DeepEqual(captured.Sandbox, expected) {
		t.Fatalf("effective sandbox spec mismatch:\\nactual=%+v\\nexpected=%+v", captured.Sandbox, expected)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(captured.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatal(err)
	}
	permission, _ := config["permission"].(map[string]any)
	if !reflect.DeepEqual(permission, map[string]any{"*": "allow", "external_directory": "deny", "webfetch": "deny", "websearch": "deny"}) {
		t.Fatalf("unsafe OpenCode permissions: %#v", permission)
	}
	outerEnv := []string{
		"PATH=" + openCodeDefaultToolPath,
		"HOME=" + filepath.Join(runtimeDir, "home"),
		"USER=mcpedge", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TERM=dumb", "SHELL=/bin/sh",
	}
	if !reflect.DeepEqual(captured.Env, outerEnv) {
		t.Fatalf("unexpected Bubblewrap parent environment: %q", captured.Env)
	}
	for _, mount := range captured.Sandbox.Mounts {
		for _, forbidden := range []string{fixture.state, "/root", "/mnt/c", "/mnt/d", "/run/docker.sock", "/var/run/docker.sock"} {
			if mount.Source == forbidden || mount.Target == forbidden {
				t.Fatalf("sandbox exposed forbidden mount %q: %+v", forbidden, mount)
			}
		}
	}
	fixture.remote.mu.Lock()
	defer fixture.remote.mu.Unlock()
	if fixture.remote.startedCalls != 1 || fixture.remote.completedCalls != 1 || fixture.remote.failedCalls != 0 {
		t.Fatalf("remote calls started=%d completed=%d failed=%d", fixture.remote.startedCalls, fixture.remote.completedCalls, fixture.remote.failedCalls)
	}
	wantPhases := []modelturn.RuntimePhase{modelturn.RuntimePhaseLeaseRetry, modelturn.RuntimePhaseLocalPreflightComplete, modelturn.RuntimePhaseDriverSocketReady, modelturn.RuntimePhaseOpenCodeProcessStarted}
	if !reflect.DeepEqual(fixture.remote.phases, wantPhases) {
		t.Fatalf("runtime phases=%v want=%v", fixture.remote.phases, wantPhases)
	}
	if fixture.remote.retries[modelturn.RuntimeRetryGatewayTimeout] != 2 {
		t.Fatalf("runtime retries=%v", fixture.remote.retries)
	}
	journalInfo, err := os.Stat(filepath.Join(fixture.state, openCodeRuntimeJournalFile))
	if err != nil || journalInfo.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode=%v err=%v", infoMode(journalInfo), err)
	}
}

func TestOpenCodeLauncherRejectsWorkspaceChangesBeforeLaunch(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		lease := fixture.lease
		lease.WorkspaceID = "ws_cccccccccccccccccccccccccccccccc"
		if _, err := fixture.launcher.RunLease(context.Background(), lease); err == nil {
			t.Fatal("missing workspace accepted")
		}
	})

	t.Run("deleted after journal", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		calls := 0
		fixture.launcher.resolveWorkspace = func(id string) (string, error) {
			calls++
			if calls == 2 {
				if err := os.RemoveAll(fixture.workspace); err != nil {
					t.Fatal(err)
				}
			}
			return fixture.registry.Resolve(id)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || calls != 2 {
			t.Fatalf("deleted workspace err=%v calls=%d", err, calls)
		}
	})

	t.Run("converted to symlink", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		calls := 0
		fixture.launcher.resolveWorkspace = func(id string) (string, error) {
			calls++
			if calls == 2 {
				target := fixture.workspace + "-real"
				if err := os.Rename(fixture.workspace, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, fixture.workspace); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			return fixture.registry.Resolve(id)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || calls != 2 {
			t.Fatalf("symlink workspace err=%v calls=%d", err, calls)
		}
	})

	t.Run("owner changed", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		calls := 0
		fixture.launcher.resolveWorkspace = func(id string) (string, error) {
			calls++
			if calls == 2 {
				return "", errors.New("workspace owner changed")
			}
			return fixture.registry.Resolve(id)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || calls != 2 {
			t.Fatalf("owner change err=%v calls=%d", err, calls)
		}
	})
}

func TestOpenCodeRuntimeJournalPreventsConcurrentAndDuplicateExecution(t *testing.T) {
	t.Run("active workspace", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		first := fixture.lease
		if _, created, err := fixture.journal.Begin(context.Background(), first.RuntimeID, first.WorkspaceID, first.GoalDigest, first.ProviderProfile); err != nil || !created {
			t.Fatalf("begin first created=%t err=%v", created, err)
		}
		second := first
		second.RuntimeID = "mr_dddddddddddddddddddddddddddddddd"
		second.Goal = "A different bounded task."
		sum := sha256.Sum256([]byte(second.Goal))
		second.GoalDigest = "sha256:" + hex.EncodeToString(sum[:])
		if _, err := fixture.launcher.RunLease(context.Background(), second); err == nil || !strings.Contains(err.Error(), "active") {
			t.Fatalf("second active workspace err=%v", err)
		}
		fixture.remote.mu.Lock()
		failedCalls := fixture.remote.failedCalls
		fixture.remote.mu.Unlock()
		if failedCalls != 1 {
			t.Fatalf("remote failed calls=%d", failedCalls)
		}
	})

	t.Run("repeat completed objective", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		first := fixture.lease
		if _, created, err := fixture.journal.Begin(context.Background(), first.RuntimeID, first.WorkspaceID, first.GoalDigest, first.ProviderProfile); err != nil || !created {
			t.Fatalf("begin first created=%t err=%v", created, err)
		}
		if err := fixture.journal.Finish(context.Background(), first.RuntimeID, OpenCodeLocalCompleted, 0, false); err != nil {
			t.Fatal(err)
		}
		second := first
		second.RuntimeID = "mr_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		if _, created, err := fixture.journal.Begin(context.Background(), second.RuntimeID, second.WorkspaceID, second.GoalDigest, second.ProviderProfile); err != nil || !created {
			t.Fatalf("repeat objective created=%t err=%v", created, err)
		}
	})
}

func TestOpenCodeLauncherRejectsLocalInstallationDrift(t *testing.T) {
	t.Run("wrong version", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		writeOpenCodeVersionScript(t, fixture.executable, "1.18.2", "")
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("wrong version err=%v", err)
		}
	})

	t.Run("bubblewrap writable", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if err := os.Chmod(fixture.bubblewrap, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "bubblewrap") {
			t.Fatalf("unsafe Bubblewrap err=%v", err)
		}
	})

	t.Run("provider absent", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if err := os.RemoveAll(fixture.provider); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "driver") {
			t.Fatalf("missing provider err=%v", err)
		}
	})

	t.Run("integrity changed", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if err := os.WriteFile(fixture.lock, []byte(`{"packages":{"node_modules/opencode-ai":{"version":"1.18.1","integrity":"sha512-wrong"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "integrity") {
			t.Fatalf("changed integrity err=%v", err)
		}
	})
}

func TestVerifyProviderPackageAcceptsResolvedReleaseSymlink(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release-provider")
	if err := os.Mkdir(release, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"@mcp-devbox/opencode-external-driver","version":"1.0.0","exports":"./index.js"}`
	if err := os.WriteFile(filepath.Join(release, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	compatibility := filepath.Join(t.TempDir(), "opencode-provider")
	if err := os.Symlink(release, compatibility); err != nil {
		t.Fatal(err)
	}
	if err := verifyProviderPackage(compatibility); err != nil {
		t.Fatalf("signed release compatibility symlink rejected: %v", err)
	}
}

func TestOpenCodeLauncherRejectsUnsafeBubblewrapAndSandboxLayouts(t *testing.T) {
	t.Run("bubblewrap absent", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if err := os.Remove(fixture.bubblewrap); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "bubblewrap") {
			t.Fatalf("missing Bubblewrap err=%v", err)
		}
	})

	t.Run("bubblewrap relative", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		config := fixture.launcher.config
		config.BubblewrapPath = "bwrap"
		if _, err := NewOpenCodeLauncher(config); err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("relative Bubblewrap err=%v", err)
		}
	})

	t.Run("bubblewrap symlink", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		target := fixture.bubblewrap + "-real"
		if err := os.Rename(fixture.bubblewrap, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, fixture.bubblewrap); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "bubblewrap") {
			t.Fatalf("symlink Bubblewrap err=%v", err)
		}
	})

	for name, mode := range map[string]os.FileMode{
		"bubblewrap not executable": 0o644,
		"bubblewrap group writable": 0o775,
		"bubblewrap other writable": 0o757,
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newOpenCodeLauncherFixture(t)
			if err := os.Chmod(fixture.bubblewrap, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "bubblewrap") {
				t.Fatalf("unsafe Bubblewrap mode=%#o err=%v", mode, err)
			}
		})
	}

	t.Run("OpenCode executable unresolvable", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if err := os.Remove(fixture.executable); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || !strings.Contains(err.Error(), "OpenCode") {
			t.Fatalf("missing OpenCode err=%v", err)
		}
	})

	t.Run("workspace and runtime overlap", func(t *testing.T) {
		root := t.TempDir()
		_, err := openCodeBubblewrapArgs("/bin/true", filepath.Join(root, "provider"), root, filepath.Join(root, "workspace"), ModelRuntimeLease{}, "{}", openCodeDefaultToolPath)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("workspace/runtime overlap err=%v", err)
		}
	})

	t.Run("provider inside runtime", func(t *testing.T) {
		root := t.TempDir()
		_, err := openCodeBubblewrapArgs("/bin/true", filepath.Join(root, "provider"), root, t.TempDir(), ModelRuntimeLease{}, "{}", openCodeDefaultToolPath)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("provider/runtime overlap err=%v", err)
		}
	})

	t.Run("runtime inside provider", func(t *testing.T) {
		provider := t.TempDir()
		_, err := openCodeBubblewrapArgs("/bin/true", provider, filepath.Join(provider, "runtime"), t.TempDir(), ModelRuntimeLease{}, "{}", openCodeDefaultToolPath)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("runtime/provider overlap err=%v", err)
		}
	})

	t.Run("provider and workspace overlap", func(t *testing.T) {
		workspace := t.TempDir()
		_, err := openCodeBubblewrapArgs("/bin/true", filepath.Join(workspace, "provider"), t.TempDir(), workspace, ModelRuntimeLease{}, "{}", openCodeDefaultToolPath)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("provider/workspace overlap err=%v", err)
		}
	})

	t.Run("socket escapes runtime", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		runtimeDir := filepath.Join(fixture.state, "r", "manual")
		if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.launcher.processSpec(runtimeDir, fixture.workspace, filepath.Join(fixture.state, "escaped.sock"), fixture.lease, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "escaped") {
			t.Fatalf("escaped socket err=%v", err)
		}
	})

	t.Run("arbitrary tool path", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		config := fixture.launcher.config
		config.ToolPath = "/tmp/unmanaged-tools"
		if _, err := NewOpenCodeLauncher(config); err == nil || !strings.Contains(err.Error(), "allowlist") {
			t.Fatalf("arbitrary tool path err=%v", err)
		}
	})

	t.Run("relative tool path", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		config := fixture.launcher.config
		config.ToolPath = "bin:/usr/bin"
		if _, err := NewOpenCodeLauncher(config); err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("relative tool path err=%v", err)
		}
	})
}

func TestOpenCodeLauncherFailsClosedWhenBubblewrapCannotCreateNamespaces(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	fixture.launcher.verifySandbox = func(context.Context, openCodeProcessSpec) error {
		return errors.New("bubblewrap could not create the required OpenCode sandbox")
	}
	directOpenCodeRan := false
	fixture.launcher.runProcess = func(context.Context, openCodeProcessSpec) openCodeProcessResult {
		directOpenCodeRan = true
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
	if err == nil || !strings.Contains(err.Error(), "bubblewrap") || result.State != OpenCodeLocalFailed {
		t.Fatalf("fail-closed result=%+v err=%v", result, err)
	}
	if directOpenCodeRan {
		t.Fatal("OpenCode ran after Bubblewrap verification failed")
	}
}

func TestOpenCodeSandboxSpecRejectsPrivateStateAndUnexpectedWritableMounts(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	runtimeDir := filepath.Join(fixture.state, "r", "manual")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := fixture.launcher.processSpec(runtimeDir, fixture.workspace, filepath.Join(runtimeDir, openCodeDriverSocketName), fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOpenCode, err := filepath.EvalSymlinks(fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range spec.Sandbox.Mounts {
		if mount.Source == fixture.state {
			t.Fatalf("private Edge state exposed: %+v", mount)
		}
		if mount.Target == openCodeSandboxProvider && mount.Writable {
			t.Fatalf("provider is writable: %+v", mount)
		}
	}
	if !spec.Sandbox.UnshareAll || !spec.Sandbox.ClearEnv {
		t.Fatalf("network/environment isolation missing: %+v", spec.Sandbox)
	}
	bad := spec.Sandbox
	bad.Mounts = append(append([]openCodeSandboxMount(nil), bad.Mounts...), openCodeSandboxMount{Source: fixture.state, Target: "/edge-state", Kind: "bind"})
	if err := validateOpenCodeSandboxSpec(bad, fixture.state, runtimeDir, fixture.workspace, fixture.provider, resolvedOpenCode, openCodeDefaultToolPath, fixture.lease); err == nil {
		t.Fatal("private Edge state mount accepted")
	}
	bad = spec.Sandbox
	bad.Mounts = append(append([]openCodeSandboxMount(nil), bad.Mounts...), openCodeSandboxMount{Source: t.TempDir(), Target: "/extra", Writable: true, Kind: "bind"})
	if err := validateOpenCodeSandboxSpec(bad, fixture.state, runtimeDir, fixture.workspace, fixture.provider, resolvedOpenCode, openCodeDefaultToolPath, fixture.lease); err == nil {
		t.Fatal("unexpected writable mount accepted")
	}
	bad = spec.Sandbox
	bad.Command = append(append([]string(nil), bad.Command...), "--help")
	if err := validateOpenCodeSandboxSpec(bad, fixture.state, runtimeDir, fixture.workspace, fixture.provider, resolvedOpenCode, openCodeDefaultToolPath, fixture.lease); err == nil {
		t.Fatal("mutated OpenCode command accepted")
	}
}

func TestValidateOpenCodeSandboxConfigRejectsDrift(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	runtimeDir := filepath.Join(fixture.state, "r", "config")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := fixture.launcher.processSpec(runtimeDir, fixture.workspace, filepath.Join(runtimeDir, openCodeDriverSocketName), fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	valid := spec.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"]
	if err := validateOpenCodeSandboxConfig(valid, fixture.lease); err != nil {
		t.Fatal(err)
	}
	mutate := func(t *testing.T, update func(map[string]any)) {
		t.Helper()
		var config map[string]any
		if err := json.Unmarshal([]byte(valid), &config); err != nil {
			t.Fatal(err)
		}
		update(config)
		encoded, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateOpenCodeSandboxConfig(string(encoded), fixture.lease); err == nil {
			t.Fatal("unsafe OpenCode configuration was accepted")
		}
	}
	t.Run("unexpected top-level field", func(t *testing.T) {
		mutate(t, func(config map[string]any) { config["plugin"] = []any{"remote"} })
	})
	t.Run("websearch allowed", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["permission"].(map[string]any)["websearch"] = "allow"
		})
	})
	t.Run("runtime identity mismatch", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["provider"].(map[string]any)["bridge"].(map[string]any)["options"].(map[string]any)["runtimeID"] = "other-runtime"
		})
	})
	t.Run("host socket", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["provider"].(map[string]any)["bridge"].(map[string]any)["options"].(map[string]any)["socketPath"] = filepath.Join(runtimeDir, openCodeDriverSocketName)
		})
	})
	t.Run("host provider", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["provider"].(map[string]any)["bridge"].(map[string]any)["npm"] = "file://" + fixture.provider
		})
	})
	t.Run("unexpected provider option", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["provider"].(map[string]any)["bridge"].(map[string]any)["options"].(map[string]any)["token"] = "forbidden"
		})
	})
	t.Run("autoupdate enabled", func(t *testing.T) {
		mutate(t, func(config map[string]any) { config["autoupdate"] = true })
	})
	t.Run("timeout mismatch", func(t *testing.T) {
		mutate(t, func(config map[string]any) {
			config["provider"].(map[string]any)["bridge"].(map[string]any)["options"].(map[string]any)["timeoutMs"] = float64(1)
		})
	})
}

func TestOpenCodePrivateSocketRejectsUnsafeMode(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "driver.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForPrivateDriverSocket(ctx, path, os.Geteuid()); err == nil {
		t.Fatal("insecure socket accepted")
	}
}

func TestWaitExternalDriverNormalizesIntentionalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/bin/sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := waitExternalDriver(ctx, cmd); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v, want context canceled", err)
	}

	unexpected := exec.Command("/bin/false")
	if err := unexpected.Start(); err != nil {
		t.Fatal(err)
	}
	if err := waitExternalDriver(context.Background(), unexpected); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected exit normalized incorrectly: %v", err)
	}
}

func TestOpenCodeLauncherHandlesProcessFailureCancellationTimeoutAndOutput(t *testing.T) {
	t.Run("unexpected termination", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		fixture.launcher.runProcess = func(context.Context, openCodeProcessSpec) openCodeProcessResult {
			return openCodeProcessResult{ExitCode: 7, Err: errors.New("exit 7")}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if err == nil || result.State != OpenCodeLocalFailed || result.ExitCode != 7 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("remote cancellation", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		fixture.remote.heartbeatState = modelturn.RuntimeStateCancelled
		fixture.launcher.runProcess = func(ctx context.Context, _ openCodeProcessSpec) openCodeProcessResult {
			<-ctx.Done()
			return openCodeProcessResult{ExitCode: -1, Err: ctx.Err()}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if !errors.Is(err, context.Canceled) || result.State != OpenCodeLocalCancelled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		fixture.lease.TimeoutSeconds = 1
		fixture.launcher.runProcess = func(ctx context.Context, _ openCodeProcessSpec) openCodeProcessResult {
			<-ctx.Done()
			return openCodeProcessResult{ExitCode: -1, Err: ctx.Err()}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if !errors.Is(err, context.DeadlineExceeded) || result.State != OpenCodeLocalFailed {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("kill switch", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		fixture.launcher.runProcess = func(ctx context.Context, _ openCodeProcessSpec) openCodeProcessResult {
			go func() {
				time.Sleep(50 * time.Millisecond)
				_ = os.WriteFile(fixture.launcher.config.StopPath, []byte("stop\n"), 0o600)
			}()
			<-ctx.Done()
			return openCodeProcessResult{ExitCode: -1, Err: ctx.Err()}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if !errors.Is(err, ErrKillSwitch) || result.State != OpenCodeLocalCancelled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("output too large", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
			_, _ = spec.Stdout.Write([]byte(strings.Repeat("x", 8192)))
			return openCodeProcessResult{ExitCode: 0}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if err != nil || result.State != OpenCodeLocalCompleted || !result.OutputTruncated {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func TestOpenCodeLauncherRestartAndTerminalReplayDoNotExecuteTwice(t *testing.T) {
	t.Run("restart marks interrupted", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		if _, created, err := fixture.journal.Begin(context.Background(), fixture.lease.RuntimeID, fixture.lease.WorkspaceID, fixture.lease.GoalDigest, fixture.lease.ProviderProfile); err != nil || !created {
			t.Fatalf("begin created=%t err=%v", created, err)
		}
		if err := fixture.journal.MarkRunning(context.Background(), fixture.lease.RuntimeID); err != nil {
			t.Fatal(err)
		}
		if err := fixture.journal.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenOpenCodeRuntimeJournal(fixture.state)
		if err != nil {
			t.Fatal(err)
		}
		fixture.journal = reopened
		fixture.launcher.config.Journal = reopened
		called := false
		fixture.launcher.runProcess = func(context.Context, openCodeProcessSpec) openCodeProcessResult {
			called = true
			return openCodeProcessResult{}
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if !errors.Is(err, ErrOpenCodeInterrupted) || called || result.State != OpenCodeLocalFailed {
			t.Fatalf("result=%+v called=%t err=%v", result, called, err)
		}
	})

	t.Run("completed runtime", func(t *testing.T) {
		fixture := newOpenCodeLauncherFixture(t)
		calls := 0
		fixture.launcher.runProcess = func(context.Context, openCodeProcessSpec) openCodeProcessResult {
			calls++
			return openCodeProcessResult{ExitCode: 0}
		}
		if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err != nil {
			t.Fatal(err)
		}
		result, err := fixture.launcher.RunLease(context.Background(), fixture.lease)
		if !errors.Is(err, ErrOpenCodeTerminal) || calls != 1 || result.State != OpenCodeLocalCompleted {
			t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
		}
	})
}

func TestBoundedSinkFailureSignalDoesNotExposeRawOutput(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"error: unknown argument --bad /private/path", "cli"},
		{"Cannot find package secret-provider from /private/path", "provider_load"},
		{"connect: no such file or directory", "driver_connect"},
		{"EACCES permission denied", "permission_other"},
		{"EACCES permission denied, open '/private/path'", "permission_open"},
		{"EPERM operation not permitted: ptrace /private/path", "permission_ptrace"},
		{"EACCES permission denied, connect /private/socket", "permission_connect"},
		{"EACCES permission denied, mkdir /private/path", "permission_mkdir"},
		{"EACCES permission denied, spawn /private/tool", "permission_spawn"},
		{"invalid config at /private/path", "config"},
		{"unknown model bridge/external-model", "model"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"ENOENT: no such file or directory, open '/private/path'"}}}`, "not_found"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"Cannot find package secret-provider from /private/path"}}}`, "provider_load"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"invalid config at /private/path"}}}`, "config"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"runtime_status"}}}`, "runtime_status"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"request_stage"}}}`, "request_stage"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"turn_create"}}}`, "turn_create"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"response_wait"}}}`, "response_wait"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"response_identity"}}}`, "response_identity"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"TypeError: cannot read properties of undefined"}}}`, "unknown_type"},
		{`{"type":"error","error":{"name":"UnknownError","data":{"message":"provider operation failed at /private/path"}}}`, "provider"},
		{`{"type":"error","message":"private"}`, "runtime_error"},
		{"opaque output", "unknown"},
	}
	for _, test := range cases {
		sink := newBoundedSink(4096)
		if _, err := sink.Write([]byte(test.text)); err != nil {
			t.Fatal(err)
		}
		if got := sink.FailureSignal(); got != test.want {
			t.Fatalf("output=%q signal=%q want=%q", test.text, got, test.want)
		}
		if strings.Contains(sink.FailureSignal(), "/private/path") || strings.Contains(sink.FailureSignal(), "secret-provider") {
			t.Fatalf("failure signal leaked raw output: %q", sink.FailureSignal())
		}
	}
}

func TestOpenCodeLauncherRefusesRootExecution(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	fixture.launcher.allowRootTest = false
	fixture.launcher.effectiveUID = func() int { return 0 }
	called := false
	fixture.launcher.runProcess = func(context.Context, openCodeProcessSpec) openCodeProcessResult {
		called = true
		return openCodeProcessResult{}
	}
	if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err == nil || called {
		t.Fatalf("root execution err=%v called=%t", err, called)
	}
}

func TestOpenCodeRuntimeJournalContainsNoSensitiveExecutionData(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	if _, err := fixture.launcher.RunLease(context.Background(), fixture.lease); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(fixture.state, openCodeRuntimeJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{fixture.workspace, fixture.lease.Goal, "OPENCODE_CONFIG_CONTENT", "OPENAI", "ANTHROPIC"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("journal contains forbidden execution data %q", forbidden)
		}
	}
	var tables []string
	rows, err := fixture.journal.db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	encoded, _ := json.Marshal(tables)
	if string(encoded) != `["local_opencode_runtimes"]` {
		t.Fatalf("journal tables=%s", encoded)
	}
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}
