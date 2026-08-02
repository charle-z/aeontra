//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
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
	aliveErr error
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
	identity := ProjectProcessIdentity{ProcessID: spec.PersistentProcessID, PID: platform.nextPID, ProcessGroupID: platform.nextPID, StartTicks: uint64(platform.nextPID * 10)}
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
	if process != nil && process.aliveErr != nil {
		return false, process.aliveErr
	}
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
	if strings.Contains(joined, "--die-with-parent") {
		t.Fatalf("durable process remained parent-bound: %s", joined)
	}
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

func TestProjectProcessManagerReconcilesLiveAndExitedProcessesAfterRestart(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	stateRoot := t.TempDir()
	next := 0
	open := func() *ProjectProcessManager {
		manager, err := OpenProjectProcessManager(ProjectProcessManagerConfig{
			StateRoot: stateRoot, Platform: platform, MaxProcesses: 16, MaxLogBytes: 1 << 20,
			NewID: func() (string, error) {
				next++
				return "pr_0123456789abcdef0123456789abcde" + string(rune('0'+next)), nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return manager
	}
	first := open()
	request := testProjectProcessRequest(t, "restart-live")
	started, _, err := first.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	recovered := open()
	status, err := recovered.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024})
	if err != nil || status.State != ProjectProcessRunning {
		t.Fatalf("recovered live status=%+v err=%v", status, err)
	}
	platform.mu.Lock()
	platform.processes[startedProcessPID(platform)].alive = false
	platform.mu.Unlock()
	status, err = recovered.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024})
	if err != nil || status.State != ProjectProcessExited || status.ExitKnown || status.Reason != "process_exited_while_offline" {
		t.Fatalf("reconciled exited status=%+v err=%v", status, err)
	}
	_ = recovered.Close()
}

func TestProjectProcessManagerListsSignalsAndCleansOnlyTerminalRecords(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	first, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "list-1"))
	second, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "list-2"))

	items, err := manager.List(ProjectProcessListRequest{ProjectAlias: "project", TargetAlias: "parrot", Limit: 10})
	if err != nil || len(items) != 2 || items[0].ProcessID == "" || items[0].StartedAt.IsZero() {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if _, err := manager.Signal(ProjectProcessSignalRequest{ProcessID: first.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", Signal: ProjectProcessInterrupt}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(platform.signals, []ProjectProcessSignal{ProjectProcessInterrupt}) {
		t.Fatalf("signals=%v", platform.signals)
	}
	cleaned, err := manager.Cleanup(ProjectProcessCleanupRequest{ProcessID: first.ProcessID, ProjectAlias: "project", TargetAlias: "parrot"})
	if err != nil || cleaned.Removed != 0 || cleaned.Active != 1 {
		t.Fatalf("live cleanup=%+v err=%v", cleaned, err)
	}
	platform.naturalExit(platform.nextPID, 0)
	waitProjectProcessState(t, manager, second.ProcessID, ProjectProcessExited)
	cleaned, err = manager.Cleanup(ProjectProcessCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot"})
	if err != nil || cleaned.Removed != 1 || cleaned.Active != 1 {
		t.Fatalf("project cleanup=%+v err=%v", cleaned, err)
	}
	if _, err := manager.Status(ProjectProcessReadRequest{ProcessID: second.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024}); !errors.Is(err, ErrProjectProcessNotFound) {
		t.Fatalf("cleaned process remained: %v", err)
	}
}

func TestProjectProcessManagerRestartRejectsReusedPID(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	started, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "restart-reuse"))
	manager.watchMu.Lock()
	delete(manager.watching, started.ProcessID)
	manager.watchMu.Unlock()
	platform.mu.Lock()
	platform.processes[platform.nextPID].identity.StartTicks++
	platform.mu.Unlock()
	if err := manager.Reconcile(); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024})
	if err != nil || status.State != ProjectProcessFailed || status.Reason != "process_identity_changed" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestProjectProcessManagerReconciliationClassifiesUnsafeOwnershipAndLogs(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	owned, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "restart-owner"))
	manager.watchMu.Lock()
	delete(manager.watching, owned.ProcessID)
	manager.watchMu.Unlock()
	platform.mu.Lock()
	platform.processes[platform.nextPID].aliveErr = ErrProjectProcessNotOwned
	platform.mu.Unlock()
	if err := manager.Reconcile(); err != nil {
		t.Fatal(err)
	}
	status, _ := manager.Status(ProjectProcessReadRequest{ProcessID: owned.ProcessID, ProjectAlias: "project", TargetAlias: "parrot", LimitBytes: 1024})
	if status.State != ProjectProcessFailed || status.Reason != "process_not_owned" {
		t.Fatalf("ownership status=%+v", status)
	}

	missing, _, _ := manager.Start(context.Background(), testProjectProcessRequest(t, "restart-logs"))
	manager.watchMu.Lock()
	delete(manager.watching, missing.ProcessID)
	manager.watchMu.Unlock()
	if err := os.Remove(filepath.Join(manager.logRoot, missing.ProcessID+".stdout.log")); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(); err != nil {
		t.Fatal(err)
	}
	record, err := manager.boundRecord(missing.ProcessID, "project", "parrot")
	if err != nil || record.State != ProjectProcessFailed || record.Reason != "process_logs_incomplete" {
		t.Fatalf("log record=%+v err=%v", record, err)
	}
}

func TestProjectProcessWorkerPersistsRedactedLogsAndExactExitReceipt(t *testing.T) {
	stateRoot := t.TempDir()
	workerRoot := filepath.Join(stateRoot, projectProcessWorkerDirectory)
	logRoot := filepath.Join(stateRoot, projectProcessLogDirectory)
	if err := os.MkdirAll(workerRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	processID := "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request := projectProcessWorkerRequest{
		Executable: "/bin/sh", Args: []string{"-c", "printf 'token=ghp_abcdefghijklmnopqrstuvwxyz0123456789AB\\n'; printf 'warning\\n' >&2; exit 7"},
		Dir: "/tmp", Env: []string{"PATH=/usr/bin:/bin", "LANG=C"}, MaxLogBytes: 1 << 20,
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, processID, "request"), body); err != nil {
		t.Fatal(err)
	}
	if err := RunProjectProcessWorker(stateRoot, processID); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.ReadFile(filepath.Join(logRoot, processID+".stdout.log"))
	if err != nil || strings.Contains(string(stdout), "ghp_") || !strings.Contains(string(stdout), "***REDACTED-SECRET***") {
		t.Fatalf("stdout=%q err=%v", stdout, err)
	}
	stderr, err := os.ReadFile(filepath.Join(logRoot, processID+".stderr.log"))
	if err != nil || string(stderr) != "warning\n" {
		t.Fatalf("stderr=%q err=%v", stderr, err)
	}
	exit, err := readProjectProcessWorkerExit(workerRoot, processID)
	if err != nil || !exit.ExitKnown || exit.ExitCode != 7 || exit.Reason != "" {
		t.Fatalf("exit=%+v err=%v", exit, err)
	}
}

func TestProjectProcessManagerRecoversIdentityWrittenBeforeJournalUpdate(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	processID := "pr_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	identity := ProjectProcessIdentity{ProcessID: processID, PID: 9001, ProcessGroupID: 9001, StartTicks: 123456}
	platform.processes[identity.PID] = &fakeProjectProcess{identity: identity, exit: make(chan ProjectProcessExit, 1), alive: true, termExit: true}
	record := projectProcessRecord{
		ProcessID: processID, IdempotencyKey: "crash-window", RequestDigest: strings.Repeat("a", 64),
		OperationID: "eo_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", TargetAlias: "parrot", State: ProjectProcessStarting, StartedAt: time.Now().UTC(),
	}
	if err := manager.insertRecord(record); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		writer, err := manager.openLogWriter(processID, stream)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeProjectProcessWorkerIdentity(manager.workerRoot, identity); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(); err != nil {
		t.Fatal(err)
	}
	recovered, err := manager.boundRecord(processID, "project", "parrot")
	if err != nil || recovered.State != ProjectProcessRunning || recovered.Identity != identity {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
}

func startedProcessPID(platform *fakeProjectProcessPlatform) int {
	return platform.nextPID
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
