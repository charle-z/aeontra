//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProjectProcess struct {
	identity ProjectProcessIdentity
	exit     chan ProjectProcessExit
	alive    bool
	termExit bool
}

type fakeProjectProcessPlatform struct {
	mu        sync.Mutex
	nextPID   int
	processes map[int]*fakeProjectProcess
	signals   []ProjectProcessSignal
	specs     []DirectWorkcellProcessSpec
	stdout    string
	stderr    string
}

func newFakeProjectProcessPlatform() *fakeProjectProcessPlatform {
	return &fakeProjectProcessPlatform{nextPID: 4000, processes: map[int]*fakeProjectProcess{}}
}

func (platform *fakeProjectProcessPlatform) Start(spec DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	platform.nextPID++
	identity := ProjectProcessIdentity{PID: platform.nextPID, ProcessGroupID: platform.nextPID, StartTicks: uint64(platform.nextPID * 10)}
	process := &fakeProjectProcess{identity: identity, exit: make(chan ProjectProcessExit, 1), alive: true, termExit: true}
	platform.processes[identity.PID] = process
	platform.specs = append(platform.specs, spec)
	_, _ = io.WriteString(spec.Stdout, platform.stdout)
	_, _ = io.WriteString(spec.Stderr, platform.stderr)
	return identity, process.exit, nil
}

func (platform *fakeProjectProcessPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	process := platform.processes[identity.PID]
	if process != nil && process.alive && process.identity != identity {
		return false, ErrProjectProcessIdentityChanged
	}
	return process != nil && process.alive, nil
}

func (platform *fakeProjectProcessPlatform) Signal(identity ProjectProcessIdentity, signal ProjectProcessSignal) error {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	process := platform.processes[identity.PID]
	if process == nil || !process.alive || process.identity != identity {
		return ErrProjectProcessIdentityChanged
	}
	platform.signals = append(platform.signals, signal)
	if signal == ProjectProcessKill || signal == ProjectProcessTerminate && process.termExit {
		process.alive = false
		process.exit <- ProjectProcessExit{ExitKnown: true, ExitCode: 0, TerminalSignal: signal}
	}
	return nil
}

func (platform *fakeProjectProcessPlatform) naturalExit(pid, code int) {
	platform.mu.Lock()
	defer platform.mu.Unlock()
	process := platform.processes[pid]
	process.alive = false
	process.exit <- ProjectProcessExit{ExitKnown: true, ExitCode: code}
}

func testProjectProcessRequest(t *testing.T, key string) ProjectProcessStartRequest {
	t.Helper()
	workspacePath := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	return ProjectProcessStartRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef", IdempotencyKey: key,
		ProjectAlias: "project", TargetAlias: "parrot",
		Workspace: Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: workspacePath, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev},
		Argv:      []string{"go", "run", "."}, Environment: map[string]string{"PORT": "8080"},
	}
}

func openTestProjectProcessManager(t *testing.T, platform *fakeProjectProcessPlatform, maxLog int64) *ProjectProcessManager {
	t.Helper()
	next := 0
	manager, err := OpenProjectProcessManager(ProjectProcessManagerConfig{
		StateRoot: t.TempDir(), Platform: platform, MaxProcesses: 16, MaxLogBytes: maxLog,
		NewID: func() (string, error) {
			next++
			return "pr_0123456789abcdef0123456789abcde" + string(rune('0'+next)), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestProjectProcessManagerStartsOnceAndReadsRedactedSeparatedOutput(t *testing.T) {
	secret := "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	platform := newFakeProjectProcessPlatform()
	platform.stdout = "ready token=" + secret + "\n"
	platform.stderr = "warning\n"
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "process-1")
	started, created, err := manager.Start(context.Background(), request)
	if err != nil || !created || started.State != ProjectProcessRunning {
		t.Fatalf("started=%+v created=%t err=%v", started, created, err)
	}
	reused, created, err := manager.Start(context.Background(), request)
	if err != nil || created || reused.ProcessID != started.ProcessID || len(platform.specs) != 1 {
		t.Fatalf("reused=%+v created=%t specs=%d err=%v", reused, created, len(platform.specs), err)
	}
	conflict := request
	conflict.Argv = []string{"go", "run", "./other"}
	if _, _, err := manager.Start(context.Background(), conflict); !errors.Is(err, ErrProjectProcessIdempotencyConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	status, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 4096})
	if err != nil || !strings.Contains(status.Stdout, "***REDACTED-SECRET***") || strings.Contains(status.Stdout, secret) || status.Stderr != "warning\n" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	next, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", StdoutOffset: status.StdoutNext, StderrOffset: status.StderrNext, LimitBytes: 4096})
	if err != nil || next.Stdout != "" || next.Stderr != "" || !next.StdoutEOF || !next.StderrEOF {
		t.Fatalf("incremental status=%+v err=%v", next, err)
	}
	joined := strings.Join(platform.specs[0].Args, "\n")
	for _, forbidden := range []string{"sh\n-c", "/root", "/mnt/c", "/var/run/docker.sock"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sandbox spec exposed %q: %s", forbidden, joined)
		}
	}
}

func TestProjectProcessLogWriterRedactsSecretsAcrossWriteBoundaries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "process.stdout.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := &projectProcessLogWriter{file: file, marker: path + ".truncated", max: 1 << 20}
	secret := "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	privateKey := "-----BEGIN RSA PRIVATE KEY-----\nprivate-material\n-----END RSA PRIVATE KEY-----\n"
	parts := []string{"token=ghp_abcdefgh", "ijklmnopqrstuvwxyz0123456789AB\n", privateKey[:21], privateKey[21:48], privateKey[48:]}
	for _, part := range parts {
		if _, err := writer.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	output := string(content)
	if strings.Contains(output, secret) || strings.Contains(output, "private-material") || strings.Contains(output, "PRIVATE KEY") {
		t.Fatalf("persisted output exposed a secret: %q", output)
	}
	if strings.Count(output, "***REDACTED-SECRET***") != 2 {
		t.Fatalf("persisted output did not preserve two redaction markers: %q", output)
	}
}

func TestProjectProcessManagerRecordsNaturalExitAndTruncation(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	platform.stdout = strings.Repeat("x", 256)
	manager := openTestProjectProcessManager(t, platform, 64)
	started, _, err := manager.Start(context.Background(), testProjectProcessRequest(t, "process-2"))
	if err != nil {
		t.Fatal(err)
	}
	platform.naturalExit(platform.nextPID, 7)
	status := waitProjectProcessState(t, manager, started.ProcessID, ProjectProcessExited)
	if !status.ExitKnown || status.ExitCode != 7 || !status.StdoutTruncated || len(status.Stdout) > 64 || !status.StdoutEOF {
		t.Fatalf("status=%+v", status)
	}
	zero, _, err := manager.Start(context.Background(), testProjectProcessRequest(t, "process-zero"))
	if err != nil {
		t.Fatal(err)
	}
	platform.naturalExit(platform.nextPID, 0)
	zeroStatus := waitProjectProcessState(t, manager, zero.ProcessID, ProjectProcessExited)
	if !zeroStatus.ExitKnown || zeroStatus.ExitCode != 0 {
		t.Fatalf("zero status=%+v", zeroStatus)
	}
}

func TestProjectProcessManagerStopsWithTermThenKillAndIsIdempotent(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	first, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "process-3"))
	stopped, err := manager.Stop(context.Background(), ProjectProcessStopRequest{ProcessID: first.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", GracePeriod: 100 * time.Millisecond})
	if err != nil || stopped.State != ProjectProcessStopped || !slices.Equal(platform.signals, []ProjectProcessSignal{ProjectProcessTerminate}) {
		t.Fatalf("stopped=%+v signals=%v err=%v", stopped, platform.signals, err)
	}
	again, err := manager.Stop(context.Background(), ProjectProcessStopRequest{ProcessID: first.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", GracePeriod: 100 * time.Millisecond})
	if err != nil || again.State != ProjectProcessStopped || len(platform.signals) != 1 {
		t.Fatalf("again=%+v signals=%v err=%v", again, platform.signals, err)
	}
	second, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "process-4"))
	platform.processes[platform.nextPID].termExit = false
	stopped, err = manager.Stop(context.Background(), ProjectProcessStopRequest{ProcessID: second.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", GracePeriod: 20 * time.Millisecond})
	if err != nil || stopped.State != ProjectProcessStopped || !slices.Equal(platform.signals[len(platform.signals)-2:], []ProjectProcessSignal{ProjectProcessTerminate, ProjectProcessKill}) {
		t.Fatalf("stopped=%+v signals=%v err=%v", stopped, platform.signals, err)
	}
}

func TestProjectProcessManagerRejectsCrossProjectAndPIDReuse(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	started, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "process-5"))
	if _, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "other", TargetAlias: "parrot", LimitBytes: 1024}); !errors.Is(err, ErrProjectProcessNotFound) {
		t.Fatalf("cross-project err=%v", err)
	}
	if _, err := manager.Status(ProjectProcessReadRequest{ProcessID: "pr_ffffffffffffffffffffffffffffffff", ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024}); !errors.Is(err, ErrProjectProcessNotFound) {
		t.Fatalf("missing process err=%v", err)
	}
	platform.mu.Lock()
	process := platform.processes[platform.nextPID]
	process.identity.StartTicks++
	platform.mu.Unlock()
	status, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024})
	if err != nil || status.State != ProjectProcessFailed || status.Reason != "process_identity_changed" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := manager.Stop(context.Background(), ProjectProcessStopRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", GracePeriod: time.Second}); err != nil {
		t.Fatalf("terminal stop should be idempotent: %v", err)
	}
}

func TestProjectProcessManagerRejectsSymlinkedPrivateLog(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	started, _, err := manager.Start(context.Background(), testProjectProcessRequest(t, "process-log-symlink"))
	if err != nil {
		t.Fatal(err)
	}
	platform.naturalExit(platform.nextPID, 0)
	waitProjectProcessState(t, manager, started.ProcessID, ProjectProcessExited)
	logPath := filepath.Join(manager.logRoot, started.ProcessID+".stdout.log")
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(privatePath, []byte("must-not-be-returned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(privatePath, logPath); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024}); err == nil {
		t.Fatal("symlinked private process log was accepted")
	}
}

func TestProjectProcessManagerUsesForegroundCWDAndSymlinkValidation(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	invalid := testProjectProcessRequest(t, "process-cwd-invalid")
	invalid.CWD = "../outside"
	if _, _, err := manager.Start(context.Background(), invalid); !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("invalid cwd err=%v", err)
	}
	symlink := testProjectProcessRequest(t, "process-cwd-symlink")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(symlink.Workspace.Path, "escape")); err != nil {
		t.Fatal(err)
	}
	symlink.CWD = "escape"
	if _, _, err := manager.Start(context.Background(), symlink); !errors.Is(err, ErrDirectWorkcellContract) {
		t.Fatalf("symlink cwd err=%v", err)
	}
	if len(platform.specs) != 0 {
		t.Fatalf("unsafe cwd started %d processes", len(platform.specs))
	}
}

func TestProjectProcessManagerRejectsSecretMaterialBeforeStarting(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "process-secret")
	request.Environment["ACCESS_TOKEN"] = "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"
	if _, _, err := manager.Start(context.Background(), request); err == nil || !strings.Contains(err.Error(), "secret material") {
		t.Fatalf("secret request err=%v", err)
	}
	if len(platform.specs) != 0 {
		t.Fatalf("secret request started %d processes", len(platform.specs))
	}
}

func waitProjectProcessState(t *testing.T, manager *ProjectProcessManager, processID string, state ProjectProcessState) ProjectProcessSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := manager.Status(ProjectProcessReadRequest{ProcessID: processID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 4096})
		if err == nil && status.State == state {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process state did not converge")
	return ProjectProcessSnapshot{}
}
