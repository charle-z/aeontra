//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type fakeWindowsProcessPlatform struct {
	mu              sync.Mutex
	identity        ProjectProcessIdentity
	alive           bool
	exits           chan ProjectProcessExit
	frames          map[string]ProjectProcessStdinReceipt
	signals         []ProjectProcessSignal
	signalErr       error
	terminateSticks bool
}

func (p *fakeWindowsProcessPlatform) Start(DirectWorkcellProcessSpec) (ProjectProcessIdentity, <-chan ProjectProcessExit, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.identity = ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 4321, ProcessGroupID: 4321, StartTicks: 77}
	p.alive = true
	p.exits = make(chan ProjectProcessExit, 1)
	p.frames = map[string]ProjectProcessStdinReceipt{}
	return p.identity, p.exits, nil
}
func (p *fakeWindowsProcessPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if identity != p.identity {
		return false, ErrProjectProcessIdentityChanged
	}
	return p.alive, nil
}
func (p *fakeWindowsProcessPlatform) Signal(identity ProjectProcessIdentity, signal ProjectProcessSignal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if identity != p.identity {
		return ErrProjectProcessIdentityChanged
	}
	if p.signalErr != nil {
		return p.signalErr
	}
	p.signals = append(p.signals, signal)
	if signal == ProjectProcessTerminate && p.terminateSticks {
		return nil
	}
	p.alive = false
	if p.exits != nil {
		p.exits <- ProjectProcessExit{ExitKnown: true, ExitCode: 0}
	}
	return nil
}
func (p *fakeWindowsProcessPlatform) WriteStdin(identity ProjectProcessIdentity, write ProjectProcessStdinWrite) (ProjectProcessStdinReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if identity != p.identity {
		return ProjectProcessStdinReceipt{}, ErrProjectProcessIdentityChanged
	}
	if receipt, ok := p.frames[write.FrameID]; ok {
		receipt.Reused = true
		return receipt, nil
	}
	receipt := ProjectProcessStdinReceipt{NextOffset: write.ExpectedOffset + int64(len(write.Data)), AcceptedBytes: len(write.Data), Closed: write.Close}
	p.frames[write.FrameID] = receipt
	return receipt, nil
}

func openWindowsTestProcessManager(t *testing.T, platform ProjectProcessPlatform) *ProjectProcessManager {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	manager, err := OpenProjectProcessManager(ProjectProcessManagerConfig{
		StateRoot: root, Platform: platform, MaxProcesses: 2, MaxLogBytes: 1 << 20,
		NewID: func() (string, error) { return "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil },
	})
	if err != nil {
		t.Fatalf("open native process manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestWindowsProjectProcessManagerReopensPrivateJournal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	config := ProjectProcessManagerConfig{StateRoot: root, Platform: &fakeWindowsProcessPlatform{}, MaxProcesses: 2, MaxLogBytes: 1 << 20}
	manager, err := OpenProjectProcessManager(config)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close manager: %v", err)
	}
	reopened, err := OpenProjectProcessManager(config)
	if err != nil {
		t.Fatalf("reopen manager: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened manager: %v", err)
	}
}

func TestWindowsProjectProcessManagerBindsOwnershipAndReplaysStdin(t *testing.T) {
	platform := &fakeWindowsProcessPlatform{}
	manager := openWindowsTestProcessManager(t, platform)
	workspace := windowsDirectWorkcellFixture(t)
	request := ProjectProcessStartRequest{
		OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "windows-process",
		ProjectAlias: "project", TargetAlias: "windows", Workspace: workspace,
		Argv: []string{"cmd.exe", "/c", "echo", "ok"}, Stdin: "initial\n",
	}
	started, created, err := manager.Start(context.Background(), request)
	if err != nil || !created || started.State != ProjectProcessRunning {
		t.Fatalf("start=%+v created=%v err=%v", started, created, err)
	}
	if _, err := manager.Status(ProjectProcessReadRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "windows", LimitBytes: 1024}); err != nil {
		t.Fatalf("status after start: %v", err)
	}
	first := ProjectProcessStdinRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "windows", FrameID: "frame-1", ExpectedOffset: int64(len(request.Stdin)), Data: "next\n"}
	if _, receipt, err := manager.WriteStdin(first); err != nil || receipt.NextOffset != int64(len(request.Stdin)+len(first.Data)) {
		t.Fatalf("first stdin=%+v err=%v", receipt, err)
	}
	if _, receipt, err := manager.WriteStdin(first); err != nil || !receipt.Reused {
		t.Fatalf("replay stdin=%+v err=%v", receipt, err)
	}
	if _, _, err := manager.WriteStdin(ProjectProcessStdinRequest{ProcessID: started.ProcessID, ProjectAlias: "other", TargetAlias: "windows", FrameID: "frame-2", ExpectedOffset: first.ExpectedOffset, Data: "x"}); !errors.Is(err, ErrProjectProcessNotFound) {
		t.Fatalf("cross-project stdin err=%v", err)
	}
	stopped, err := manager.Stop(context.Background(), ProjectProcessStopRequest{ProcessID: started.ProcessID, ProjectAlias: "project", TargetAlias: "windows", GracePeriod: time.Second})
	if err != nil || stopped.State != ProjectProcessStopped {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
}

func TestWindowsProjectProcessManagerRejectsInitialStdinOverTotalLimit(t *testing.T) {
	manager := openWindowsTestProcessManager(t, &fakeWindowsProcessPlatform{})
	request := ProjectProcessStartRequest{OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "too-large", ProjectAlias: "project", TargetAlias: "windows", Workspace: Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Profile: WorkspaceProfileWindowsWorkcell, Mode: WorkspaceModeDev, Path: filepath.FromSlash("C:/work/repo"), WindowsDevRoot: filepath.FromSlash("C:/work")}, Argv: []string{"cmd.exe"}, Stdin: string(make([]byte, edge.MaxProjectProcessStdinTotalBytes+1))}
	if _, _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("oversized initial stdin was accepted")
	}
}

func TestWindowsProjectProcessReconcileContinuesExactStoppingIntent(t *testing.T) {
	platform := &fakeWindowsProcessPlatform{
		identity:  ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 4321, ProcessGroupID: 4321, StartTicks: 77},
		alive:     true,
		signalErr: errors.New("transient signal failure"),
	}
	manager := openWindowsTestProcessManager(t, platform)
	record := projectProcessRecord{
		ProcessID:      platform.identity.ProcessID,
		IdempotencyKey: "reconcile-stopping",
		RequestDigest:  strings.Repeat("a", 64),
		OperationID:    "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkspaceID:    "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProjectAlias:   "project",
		TargetAlias:    "windows",
		Identity:       platform.identity,
		State:          ProjectProcessStopping,
		StartedAt:      time.Now().UTC().Add(-time.Minute),
	}
	if err := manager.insertRecord(record); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=?`,
		record.Identity.PID, record.Identity.ProcessGroupID, record.Identity.StartTicks, record.State, record.ProcessID); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		writer, err := manager.openLogWriter(record.ProcessID, stream)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Reconcile(); err != nil {
		t.Fatalf("reconcile stopping process: %v", err)
	}
	got, err := manager.recordByID(record.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	platform.mu.Lock()
	signals := append([]ProjectProcessSignal(nil), platform.signals...)
	platform.signalErr = nil
	platform.mu.Unlock()
	if got.State != ProjectProcessStopping || len(signals) != 0 {
		t.Fatalf("transient reconciliation record=%+v signals=%v", got, signals)
	}
	if err := manager.Reconcile(); err != nil {
		t.Fatalf("retry stopping process: %v", err)
	}
	got, err = manager.recordByID(record.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != ProjectProcessStopped || got.Reason != "process_stopped_while_offline" {
		t.Fatalf("reconciled record=%+v", got)
	}
	platform.mu.Lock()
	signals = append([]ProjectProcessSignal(nil), platform.signals...)
	platform.mu.Unlock()
	if len(signals) != 1 || signals[0] != ProjectProcessTerminate {
		t.Fatalf("signals=%v want=[%s]", signals, ProjectProcessTerminate)
	}
}

func TestWindowsProjectProcessStopReconcilesUnwatchedWorker(t *testing.T) {
	platform := &fakeWindowsProcessPlatform{
		identity:        ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 4321, ProcessGroupID: 4321, StartTicks: 77},
		alive:           true,
		terminateSticks: true,
	}
	manager := openWindowsTestProcessManager(t, platform)
	record := projectProcessRecord{
		ProcessID:      platform.identity.ProcessID,
		IdempotencyKey: "stop-unwatched",
		RequestDigest:  strings.Repeat("b", 64),
		OperationID:    "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkspaceID:    "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProjectAlias:   "project",
		TargetAlias:    "windows",
		Identity:       platform.identity,
		State:          ProjectProcessRunning,
		StartedAt:      time.Now().UTC().Add(-time.Minute),
	}
	if err := manager.insertRecord(record); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.db.Exec(`UPDATE project_processes SET pid=?,process_group_id=?,start_ticks=?,state=? WHERE process_id=?`,
		record.Identity.PID, record.Identity.ProcessGroupID, record.Identity.StartTicks, record.State, record.ProcessID); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		writer, err := manager.openLogWriter(record.ProcessID, stream)
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	stopped, err := manager.Stop(ctx, ProjectProcessStopRequest{
		ProcessID: record.ProcessID, ProjectAlias: record.ProjectAlias, TargetAlias: record.TargetAlias, GracePeriod: 50 * time.Millisecond,
	})
	if err != nil || stopped.State != ProjectProcessStopped || stopped.Reason != "process_stopped_while_offline" {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
	platform.mu.Lock()
	signals := append([]ProjectProcessSignal(nil), platform.signals...)
	platform.mu.Unlock()
	if !slices.Equal(signals, []ProjectProcessSignal{ProjectProcessTerminate, ProjectProcessKill}) {
		t.Fatalf("signals=%v", signals)
	}
}

func TestWindowsProjectProcessSignalUsesExactFallbackOnlyForStopSignals(t *testing.T) {
	identity := ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 4321, ProcessGroupID: 4321, StartTicks: 77}
	for _, test := range []struct {
		name         string
		signal       ProjectProcessSignal
		response     projectProcessStdinWireResponse
		pipeErr      error
		wantFallback bool
		wantIdentity bool
		wantErr      bool
	}{
		{name: "terminate transport failure", signal: ProjectProcessTerminate, pipeErr: errors.New("pipe unavailable"), wantFallback: true},
		{name: "kill worker failure", signal: ProjectProcessKill, response: projectProcessStdinWireResponse{Error: "signal_failed"}, wantFallback: true},
		{name: "kill verifies cooperative success", signal: ProjectProcessKill, wantFallback: true},
		{name: "interrupt remains cooperative", signal: ProjectProcessInterrupt, pipeErr: errors.New("pipe unavailable"), wantErr: true},
		{name: "identity change fails closed", signal: ProjectProcessTerminate, response: projectProcessStdinWireResponse{Error: "identity_changed"}, wantIdentity: true, wantErr: true},
		{name: "cooperative success", signal: ProjectProcessTerminate},
	} {
		t.Run(test.name, func(t *testing.T) {
			fallbacks := 0
			platform := &windowsProjectProcessPlatform{
				sendControl: func(got ProjectProcessIdentity, request projectProcessControlRequest) (projectProcessStdinWireResponse, error) {
					if got != identity || request.Identity != identity || request.Signal != string(test.signal) {
						t.Fatalf("identity=%+v request=%+v", got, request)
					}
					return test.response, test.pipeErr
				},
				terminateExact: func(got ProjectProcessIdentity) error {
					fallbacks++
					if got != identity {
						t.Fatalf("fallback identity=%+v", got)
					}
					return nil
				},
			}
			err := platform.Signal(identity, test.signal)
			if (err != nil) != test.wantErr || errors.Is(err, ErrProjectProcessIdentityChanged) != test.wantIdentity {
				t.Fatalf("err=%v wantErr=%v wantIdentity=%v", err, test.wantErr, test.wantIdentity)
			}
			wantFallbacks := 0
			if test.wantFallback {
				wantFallbacks = 1
			}
			if fallbacks != wantFallbacks {
				t.Fatalf("fallbacks=%d want=%d", fallbacks, wantFallbacks)
			}
		})
	}
}

func TestTerminateWindowsProjectProcessWorkerBindsCreationTime(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_WINDOWS_TERMINATE_HELPER") == "1" {
		time.Sleep(time.Minute)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTerminateWindowsProjectProcessWorkerBindsCreationTime$")
	command.Env = append(os.Environ(), "MCP_DEVBOX_WINDOWS_TERMINATE_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	})
	identity, err := windowsWorkerIdentity(uint32(command.Process.Pid), "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("helper identity: %v", err)
	}
	stale := identity
	stale.StartTicks++
	if err := terminateWindowsProjectProcessWorkerExact(stale); !errors.Is(err, ErrProjectProcessIdentityChanged) {
		t.Fatalf("stale identity err=%v", err)
	}
	alive, err := (&windowsProjectProcessPlatform{}).Alive(identity)
	if err != nil || !alive {
		t.Fatalf("helper after stale identity alive=%v err=%v", alive, err)
	}
	if err := terminateWindowsProjectProcessWorkerExact(identity); err != nil {
		t.Fatalf("terminate helper: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("terminated helper exited successfully")
	}
	command.Process = nil
}

func TestWindowsProcessPipeNameIsPrivateNamespace(t *testing.T) {
	name := windowsProjectProcessPipeName("pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if name != `\\.\pipe\mcp-devbox-project-process-pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa` {
		t.Fatalf("pipe name=%q", name)
	}
}

func TestWindowsProcessSignalBindsExactWorkerIdentity(t *testing.T) {
	identity := ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 42, ProcessGroupID: 42, StartTicks: 99}
	request := projectProcessControlRequest{Kind: "signal", Signal: string(ProjectProcessKill), Identity: identity}
	if !validWindowsProcessControlRequest(request, identity) {
		t.Fatal("exact worker signal was rejected")
	}
	request.Identity.StartTicks++
	if validWindowsProcessControlRequest(request, identity) {
		t.Fatal("signal for another worker identity was accepted")
	}
}

func TestWindowsWorkerStdinBindsFrameOffsetDigestAndEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	state := &windowsProcessWorkerState{
		input:      writer,
		identity:   ProjectProcessIdentity{ProcessID: "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PID: 4321, ProcessGroupID: 4321, StartTicks: 77},
		workerRoot: t.TempDir(),
		done:       make(chan struct{}),
	}
	readDone := make(chan string, 1)
	go func() {
		buf := make([]byte, 4)
		_, _ = io.ReadFull(reader, buf)
		readDone <- string(buf)
	}()
	first := handleWindowsWorkerStdin(state, projectProcessStdinWireRequest{Identity: state.identity, FrameID: "frame-1", ExpectedOffset: 0, Data: "ping"})
	if first.Error != "" || first.AcceptedBytes != 4 || first.NextOffset != 4 || first.Closed {
		t.Fatalf("first=%+v", first)
	}
	if got := <-readDone; got != "ping" {
		t.Fatalf("received=%q", got)
	}
	replay := handleWindowsWorkerStdin(state, projectProcessStdinWireRequest{Identity: state.identity, FrameID: "frame-1", ExpectedOffset: 0, Data: "ping"})
	if replay.Error != "" || !replay.Reused || replay.NextOffset != 4 {
		t.Fatalf("replay=%+v", replay)
	}
	conflict := handleWindowsWorkerStdin(state, projectProcessStdinWireRequest{Identity: state.identity, FrameID: "frame-1", ExpectedOffset: 0, Data: "pong"})
	if conflict.Error != "offset_conflict" {
		t.Fatalf("conflict=%+v", conflict)
	}
}
