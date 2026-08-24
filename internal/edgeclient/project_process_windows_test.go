//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type fakeWindowsProcessPlatform struct {
	mu       sync.Mutex
	identity ProjectProcessIdentity
	alive    bool
	exits    chan ProjectProcessExit
	frames   map[string]ProjectProcessStdinReceipt
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
func (p *fakeWindowsProcessPlatform) Signal(identity ProjectProcessIdentity, _ ProjectProcessSignal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if identity != p.identity {
		return ErrProjectProcessIdentityChanged
	}
	p.alive = false
	p.exits <- ProjectProcessExit{ExitKnown: true, ExitCode: 0}
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
	root := t.TempDir()
	manager, err := OpenProjectProcessManager(ProjectProcessManagerConfig{
		StateRoot: root, Platform: platform, MaxProcesses: 2, MaxLogBytes: 1 << 20,
		NewID: func() (string, error) { return "pr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil },
	})
	if err != nil {
		t.Skipf("native process journal requires the configured Windows service ACL: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

func TestWindowsProjectProcessManagerBindsOwnershipAndReplaysStdin(t *testing.T) {
	platform := &fakeWindowsProcessPlatform{}
	manager := openWindowsTestProcessManager(t, platform)
	request := ProjectProcessStartRequest{
		OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "windows-process",
		ProjectAlias: "project", TargetAlias: "windows", Workspace: Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Profile: WorkspaceProfileWindowsWorkcell, Mode: WorkspaceModeDev, Path: `C:\work\repo`, WindowsDevRoot: `C:\work`},
		Argv: []string{"cmd.exe", "/c", "echo", "ok"}, Stdin: "initial\n",
	}
	started, created, err := manager.Start(context.Background(), request)
	if err != nil || !created || started.State != ProjectProcessRunning {
		t.Fatalf("start=%+v created=%v err=%v", started, created, err)
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
}

func TestWindowsProjectProcessManagerRejectsInitialStdinOverTotalLimit(t *testing.T) {
	manager := openWindowsTestProcessManager(t, &fakeWindowsProcessPlatform{})
	request := ProjectProcessStartRequest{OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "too-large", ProjectAlias: "project", TargetAlias: "windows", Workspace: Workspace{ID: "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Profile: WorkspaceProfileWindowsWorkcell, Mode: WorkspaceModeDev, Path: filepath.FromSlash("C:/work/repo"), WindowsDevRoot: filepath.FromSlash("C:/work")}, Argv: []string{"cmd.exe"}, Stdin: string(make([]byte, edge.MaxProjectProcessStdinTotalBytes+1))}
	if _, _, err := manager.Start(context.Background(), request); err == nil {
		t.Fatal("oversized initial stdin was accepted")
	}
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
