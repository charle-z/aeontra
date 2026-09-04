//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// concurrentToolboxRunner deliberately pauses the first lifecycle operation
// at a point where a second manager would observe the same old record if the
// managers were only protected by their private mutexes. It also serializes
// the fake engine state so the assertions remain valid under -race.
type concurrentToolboxRunner struct {
	inner *recordingToolboxRunner

	mu          sync.Mutex
	pullCalls   int
	createCalls int
	rmCalls     int

	firstPullStarted  chan struct{}
	secondPullStarted chan struct{}
	releaseFirstPull  chan struct{}
	firstRMStarted    chan struct{}
	secondRMStarted   chan struct{}
	releaseFirstRM    chan struct{}
}

func (runner *concurrentToolboxRunner) Run(ctx context.Context, executable string, args, environment []string) ([]byte, error) {
	joined := strings.Join(args, " ")
	command := " " + joined + " "
	switch {
	case strings.Contains(command, " pull "):
		runner.mu.Lock()
		runner.pullCalls++
		call := runner.pullCalls
		if call == 1 && runner.firstPullStarted != nil {
			close(runner.firstPullStarted)
		}
		if call == 2 && runner.secondPullStarted != nil {
			close(runner.secondPullStarted)
		}
		release := runner.releaseFirstPull
		runner.mu.Unlock()
		if call == 1 && release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	case strings.Contains(command, " rm "):
		runner.mu.Lock()
		runner.rmCalls++
		call := runner.rmCalls
		if call == 1 && runner.firstRMStarted != nil {
			close(runner.firstRMStarted)
		}
		if call == 2 && runner.secondRMStarted != nil {
			close(runner.secondRMStarted)
		}
		release := runner.releaseFirstRM
		runner.mu.Unlock()
		if call == 1 && release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	runner.mu.Lock()
	output, err := runner.inner.Run(ctx, executable, args, environment)
	if strings.Contains(command, " create ") {
		runner.createCalls++
	}
	runner.mu.Unlock()
	return output, err
}

func newConcurrentToolboxManager(t *testing.T, stateRoot string, workspace Workspace, runner *concurrentToolboxRunner) *ProjectToolboxManager {
	t.Helper()
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:   stateRoot,
		Endpoint:    &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(stateRoot, "podman.sock"), Executable: "/usr/bin/podman"},
		Runner:      runner,
		environment: testRootlessContainerEnvironment,
		NewID:       func() (string, error) { return "tb_11111111111111111111111111111111", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func concurrentToolboxRequest(workspace Workspace) ProjectToolboxCreateRequest {
	return ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048}
}

func waitForNoSignal(t *testing.T, signal <-chan struct{}, duration time.Duration, label string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatalf("unexpected concurrent %s", label)
	case <-time.After(duration):
	}
}

func (runner *concurrentToolboxRunner) counts() (pull, create, remove int) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.pullCalls, runner.createCalls, runner.rmCalls
}

func TestProjectToolboxConcurrentCreateUsesSharedWorkspaceLock(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_44444444444444444444444444444444", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &concurrentToolboxRunner{
		inner:            &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")},
		firstPullStarted: make(chan struct{}), secondPullStarted: make(chan struct{}), releaseFirstPull: make(chan struct{}),
	}
	managerA := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	managerB := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	request := concurrentToolboxRequest(workspace)
	type result struct {
		reused bool
		err    error
	}
	results := make(chan result, 2)
	go func() {
		_, reused, err := managerA.Create(t.Context(), request)
		results <- result{reused: reused, err: err}
	}()
	select {
	case <-runner.firstPullStarted:
	case <-time.After(time.Second):
		t.Fatal("first create did not reach pull")
	}
	go func() {
		_, reused, err := managerB.Create(t.Context(), request)
		results <- result{reused: reused, err: err}
	}()
	waitForNoSignal(t, runner.secondPullStarted, 100*time.Millisecond, "pull")
	close(runner.releaseFirstPull)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("create results: first=%+v second=%+v", first, second)
	}
	if first.reused == second.reused {
		t.Fatalf("expected exactly one creator and one reuse: first=%v second=%v", first.reused, second.reused)
	}
	pull, create, remove := runner.counts()
	if pull != 1 || create != 1 || remove != 0 {
		t.Fatalf("lifecycle calls pull=%d create=%d remove=%d", pull, create, remove)
	}
}

func TestProjectToolboxConcurrentReconcileDoesNotDoubleRecreate(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_55555555555555555555555555555555", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	inner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	runner := &concurrentToolboxRunner{inner: inner}
	manager := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	if _, _, err := manager.Create(t.Context(), concurrentToolboxRequest(workspace)); err != nil {
		t.Fatal(err)
	}
	inner.mounts = []struct {
		Type, Source, Destination string
		RW                        bool
	}{{Type: "bind", Source: filepath.Join(stateRoot, "wrong"), Destination: "/workspace", RW: true}}
	runner.firstRMStarted = make(chan struct{})
	runner.secondRMStarted = make(chan struct{})
	runner.releaseFirstRM = make(chan struct{})
	managerA := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	managerB := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	request := ProjectToolboxReconcileRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}
	results := make(chan error, 2)
	go func() { _, err := managerA.Reconcile(t.Context(), request); results <- err }()
	select {
	case <-runner.firstRMStarted:
	case <-time.After(time.Second):
		t.Fatal("first reconcile did not reach remove")
	}
	go func() { _, err := managerB.Reconcile(t.Context(), request); results <- err }()
	waitForNoSignal(t, runner.secondRMStarted, 100*time.Millisecond, "reconcile remove")
	close(runner.releaseFirstRM)
	if err := <-results; err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := <-results; err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	pull, create, remove := runner.counts()
	if pull != 1 || create != 2 || remove != 1 {
		t.Fatalf("lifecycle calls pull=%d create=%d remove=%d", pull, create, remove)
	}
}

func TestProjectToolboxConcurrentCleanupHasSingleWinner(t *testing.T) {
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_66666666666666666666666666666666", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &concurrentToolboxRunner{inner: &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}}
	manager := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	if _, _, err := manager.Create(t.Context(), concurrentToolboxRequest(workspace)); err != nil {
		t.Fatal(err)
	}
	runner.firstRMStarted = make(chan struct{})
	runner.secondRMStarted = make(chan struct{})
	runner.releaseFirstRM = make(chan struct{})
	managerA := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	managerB := newConcurrentToolboxManager(t, stateRoot, workspace, runner)
	request := ProjectToolboxCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}
	results := make(chan struct {
		removed bool
		err     error
	}, 2)
	go func() {
		removed, err := managerA.Cleanup(t.Context(), request)
		results <- struct {
			removed bool
			err     error
		}{removed, err}
	}()
	select {
	case <-runner.firstRMStarted:
	case <-time.After(time.Second):
		t.Fatal("first cleanup did not reach remove")
	}
	go func() {
		removed, err := managerB.Cleanup(t.Context(), request)
		results <- struct {
			removed bool
			err     error
		}{removed, err}
	}()
	waitForNoSignal(t, runner.secondRMStarted, 100*time.Millisecond, "cleanup remove")
	close(runner.releaseFirstRM)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("cleanup results: first=%+v second=%+v", first, second)
	}
	if first.removed == second.removed {
		t.Fatalf("expected exactly one cleanup winner: first=%v second=%v", first.removed, second.removed)
	}
	_, _, remove := runner.counts()
	if remove != 1 {
		t.Fatalf("cleanup remove calls=%d", remove)
	}
	if _, err := managerA.Status(t.Context(), ProjectToolboxStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); !errors.Is(err, ErrProjectToolboxNotFound) {
		t.Fatalf("record after cleanup err=%v", err)
	}
}
