//go:build !windows

package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

	socketRoot := filepath.Join(state, openCodeRuntimeDirName)
	launcher, err := NewOpenCodeLauncher(OpenCodeLauncherConfig{
		StateRoot: state, SocketRoot: socketRoot, OpenCodePath: executable, ProviderPath: provider, IntegrityPath: lockPath,
		OutputLimit: 4096, Heartbeat: time.Second, Workspaces: registry, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	launcher.allowRootTest = true
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
		state: state, workspace: workspace, provider: provider, executable: executable, lock: lockPath,
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
	var captured openCodeProcessSpec
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		captured = spec
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
	wantArgs := []string{"run", "--auto", "--model", openCodeModelID, "--format", "json", "--dir", fixture.workspace, fixture.lease.Goal}
	if strings.Join(captured.Args, "\x00") != strings.Join(wantArgs, "\x00") || captured.Dir != fixture.workspace || captured.Executable != fixture.executable {
		t.Fatalf("process spec=%+v", captured)
	}
	joinedEnv := strings.Join(captured.Env, "\n")
	for _, forbidden := range []string{"OPENAI", "ANTHROPIC", "OPENROUTER", "GLM", "CODEX", "API_KEY", "HTTP_PROXY", "HTTPS_PROXY"} {
		if strings.Contains(strings.ToUpper(joinedEnv), forbidden) {
			t.Fatalf("forbidden environment marker %q in %s", forbidden, joinedEnv)
		}
	}
	if !strings.Contains(joinedEnv, "OPENCODE_AUTH_CONTENT={}") || !strings.Contains(joinedEnv, "OPENCODE_DISABLE_MODELS_FETCH=1") {
		t.Fatalf("required clean environment missing: %s", joinedEnv)
	}
	fixture.remote.mu.Lock()
	defer fixture.remote.mu.Unlock()
	if fixture.remote.startedCalls != 1 || fixture.remote.completedCalls != 1 || fixture.remote.failedCalls != 0 {
		t.Fatalf("remote calls started=%d completed=%d failed=%d", fixture.remote.startedCalls, fixture.remote.completedCalls, fixture.remote.failedCalls)
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
	})

	t.Run("duplicate objective", func(t *testing.T) {
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
		if _, err := fixture.launcher.RunLease(context.Background(), second); err == nil || !strings.Contains(err.Error(), "already journaled") {
			t.Fatalf("duplicate objective err=%v", err)
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
