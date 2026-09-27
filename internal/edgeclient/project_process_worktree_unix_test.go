//go:build !windows

package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type exitBeforeTestStdinPlatform struct {
	*fakeProjectProcessPlatform
	manager *ProjectProcessManager
	once    sync.Once
	seen    bool
}

func (platform *exitBeforeTestStdinPlatform) Alive(identity ProjectProcessIdentity) (bool, error) {
	var waitErr error
	platform.once.Do(func() {
		platform.fakeProjectProcessPlatform.naturalExit(identity.PID, 0)
		_, platform.seen = platform.manager.waitTerminal(context.Background(), identity.ProcessID, time.Second)
		if !platform.seen {
			waitErr = errors.New("test process exit was not journaled")
		}
	})
	if waitErr != nil {
		return false, waitErr
	}
	return platform.fakeProjectProcessPlatform.Alive(identity)
}

func newProjectWorktreeTestProcessManagerForTest(t *testing.T, worktrees *ProjectWorktreeManager, platform ProjectProcessPlatform, generation string) (*ProjectProcessManager, *ProjectWorktreeTestProcessManager, ProjectWorktreeSnapshot, ProjectWorktreeTestProfile) {
	t.Helper()
	profilePath := filepath.Join(worktrees.stateRoot, projectWorktreeTestProfileFile)
	body, err := json.Marshal(ProjectWorktreeTestProfileDocument{
		Version: 1, ProfileID: "linux-workcell", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	processes, err := OpenProjectProcessManager(ProjectProcessManagerConfig{StateRoot: worktrees.stateRoot, Platform: platform, MaxProcesses: 8})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = processes.Close() })
	tests, err := OpenProjectWorktreeTestProcessManager(ProjectWorktreeTestProcessManagerConfig{
		StateRoot: worktrees.stateRoot, Processes: processes, Generation: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tests.Close() })
	profile, err := tests.Profile("linux-workcell")
	if err != nil {
		t.Fatal(err)
	}
	worktree, _ := newWorktreeForTestProcess(t, worktrees)
	return processes, tests, worktree, profile
}

func newWorktreeForTestProcess(t *testing.T, manager *ProjectWorktreeManager) (ProjectWorktreeSnapshot, ProjectWorktreeClaimRequest) {
	t.Helper()
	items, err := manager.List(context.Background(), "project", "parrot", 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("worktree list=%+v err=%v", items, err)
	}
	item := items[0]
	authority := ProjectWorktreeClaimRequest{ID: item.ID, JobID: item.JobID, LeaseID: item.LeaseID, Fence: item.Fence}
	return item, authority
}

func projectWorktreeTestStartRequest(worktree ProjectWorktreeSnapshot, profile ProjectWorktreeTestProfile) ProjectWorktreeTestStartRequest {
	return ProjectWorktreeTestStartRequest{
		OperationID: "eo_0123456789abcdef0123456789abcdef", IdempotencyKey: "task-test-start:0123456789abcdef0123456789abcdef",
		Alias: worktree.Alias, TargetAlias: worktree.TargetAlias, WorktreeID: worktree.ID, JobID: worktree.JobID,
		LeaseID: worktree.LeaseID, Fence: worktree.Fence, ProfileID: profile.ID, ProfileDigest: profile.Digest,
	}
}

func projectWorktreeTestReadRequest(worktree ProjectWorktreeSnapshot, profile ProjectWorktreeTestProfile, processID string) ProjectWorktreeTestReadRequest {
	return ProjectWorktreeTestReadRequest{
		ProcessID: processID, Alias: worktree.Alias, TargetAlias: worktree.TargetAlias,
		WorktreeID: worktree.ID, JobID: worktree.JobID, LeaseID: worktree.LeaseID, Fence: worktree.Fence,
		ProfileID: profile.ID, ProfileDigest: profile.Digest, LimitBytes: 1024,
	}
}

func makeProjectWorktreeTestStopRequest(worktree ProjectWorktreeSnapshot, profile ProjectWorktreeTestProfile, processID string) ProjectWorktreeTestStopRequest {
	return ProjectWorktreeTestStopRequest{
		ProcessID: processID, Alias: worktree.Alias, TargetAlias: worktree.TargetAlias,
		WorktreeID: worktree.ID, JobID: worktree.JobID, LeaseID: worktree.LeaseID, Fence: worktree.Fence,
		ProfileID: profile.ID, ProfileDigest: profile.Digest,
	}
}

func TestProjectWorktreeTestStartIsIdempotentStatusIsReadOnlyAndStopUsesCapturedBinding(t *testing.T) {
	worktrees, _, authority := newProjectWorktreeForContentDigest(t)
	platform := newFakeProjectProcessPlatform()
	_, tests, worktree, profile := newProjectWorktreeTestProcessManagerForTest(t, worktrees, platform, "edge-one")
	request := projectWorktreeTestStartRequest(worktree, profile)
	started, created, err := tests.Start(context.Background(), worktrees, request)
	if err != nil || !created || started.State != string(ProjectProcessRunning) || started.BaseCommit != worktree.BaseCommit || started.HeadCommit != worktree.BaseCommit {
		t.Fatalf("start=%+v created=%v err=%v", started, created, err)
	}
	replayed, created, err := tests.Start(context.Background(), worktrees, request)
	if err != nil || created || replayed.ProcessID != started.ProcessID || len(platform.specs) != 1 {
		t.Fatalf("replay=%+v created=%v specs=%d err=%v", replayed, created, len(platform.specs), err)
	}
	if len(platform.specs) != 1 || platform.specs[0].workspacePath != worktree.path {
		t.Fatalf("test command did not use the managed worktree: specs=%d", len(platform.specs))
	}
	if authority.Fence != worktree.Fence || authority.LeaseID != worktree.LeaseID {
		t.Fatal("test setup changed the worker authority")
	}

	if err := os.WriteFile(filepath.Join(worktree.path, "README.md"), []byte("modified during test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := tests.Status(context.Background(), worktrees, projectWorktreeTestReadRequest(worktree, profile, started.ProcessID))
	if err != nil || !status.Stale || status.StaleReason != "content_changed" || status.State != string(ProjectProcessRunning) {
		t.Fatalf("start=%+v stale status=%+v err=%v", started, status, err)
	}
	platform.mu.Lock()
	signals := append([]ProjectProcessSignal(nil), platform.signals...)
	platform.mu.Unlock()
	if len(signals) != 0 {
		t.Fatalf("read-only stale status stopped the process: %v", signals)
	}

	stopped, err := tests.Stop(context.Background(), makeProjectWorktreeTestStopRequest(worktree, profile, started.ProcessID))
	if err != nil || stopped.State != string(ProjectProcessStopped) {
		t.Fatalf("stop after worktree change=%+v err=%v", stopped, err)
	}
	if !stopped.Stale || stopped.StaleReason != "evidence_unavailable" {
		t.Fatalf("stop result incorrectly asserted fresh evidence: %+v", stopped)
	}
}

func TestProjectWorktreeTestStartAcceptsProcessExitedBeforeStdinClose(t *testing.T) {
	worktrees, _, _ := newProjectWorktreeForContentDigest(t)
	platform := &exitBeforeTestStdinPlatform{fakeProjectProcessPlatform: newFakeProjectProcessPlatform()}
	processes, tests, worktree, profile := newProjectWorktreeTestProcessManagerForTest(t, worktrees, platform, "edge-one")
	platform.manager = processes
	started, created, err := tests.Start(context.Background(), worktrees, projectWorktreeTestStartRequest(worktree, profile))
	if err != nil || !created || started.State != string(ProjectProcessExited) || !started.ExitKnown || started.ExitCode != 0 || !platform.seen {
		t.Fatalf("fast-exit start=%+v created=%v exit_observed=%v err=%v", started, created, platform.seen, err)
	}
}

func TestProjectWorktreeTestFailsClosedOnFenceRotationAndEdgeRestart(t *testing.T) {
	worktrees, _, authority := newProjectWorktreeForContentDigest(t)
	platform := newFakeProjectProcessPlatform()
	processes, tests, worktree, profile := newProjectWorktreeTestProcessManagerForTest(t, worktrees, platform, "edge-one")
	request := projectWorktreeTestStartRequest(worktree, profile)
	started, _, err := tests.Start(context.Background(), worktrees, request)
	if err != nil {
		t.Fatal(err)
	}

	rotated := ProjectWorktreeClaimRequest{ID: worktree.ID, JobID: worktree.JobID, LeaseID: "wl_abcdefabcdefabcdefabcdefabcdefab", Fence: worktree.Fence + 1}
	if _, err := worktrees.Claim(rotated); err != nil {
		t.Fatal(err)
	}
	status, err := tests.Status(context.Background(), worktrees, projectWorktreeTestReadRequest(worktree, profile, started.ProcessID))
	if err != nil || !status.Stale || status.StaleReason != "evidence_unavailable" {
		t.Fatalf("rotated-fence status=%+v err=%v", status, err)
	}
	platform.mu.Lock()
	if len(platform.signals) != 0 {
		platform.mu.Unlock()
		t.Fatalf("stale status stopped process: %v", platform.signals)
	}
	platform.mu.Unlock()

	restarted, err := OpenProjectWorktreeTestProcessManager(ProjectWorktreeTestProcessManagerConfig{
		StateRoot: worktrees.stateRoot, Processes: processes, Generation: "edge-two",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	status, err = restarted.Status(context.Background(), worktrees, projectWorktreeTestReadRequest(worktree, profile, started.ProcessID))
	if err != nil || !status.Stale || status.StaleReason != "edge_restarted" {
		t.Fatalf("restarted status=%+v err=%v", status, err)
	}
	if _, _, err := restarted.Start(context.Background(), worktrees, request); !errors.Is(err, ErrProjectWorktreeTestUnavailable) {
		t.Fatalf("replayed start after restart err=%v", err)
	}
	stopped, err := restarted.Stop(context.Background(), makeProjectWorktreeTestStopRequest(worktree, profile, started.ProcessID))
	if err != nil || stopped.State != string(ProjectProcessStopped) {
		t.Fatalf("captured-binding stop after restart=%+v err=%v", stopped, err)
	}
	if authority.ID != worktree.ID || authority.Fence != worktree.Fence {
		t.Fatal("test setup changed the captured authority")
	}
}

func TestProjectWorktreeTestProfileRequiresOwnerOnlyCanonicalFile(t *testing.T) {
	stateRoot := t.TempDir()
	profilePath := filepath.Join(stateRoot, projectWorktreeTestProfileFile)
	body := `{"version":1,"profile_id":"x","argv":["go","test"],"timeout_seconds":30}`
	if err := os.WriteFile(profilePath, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectWorktreeTestProfile(stateRoot); !errors.Is(err, ErrProjectWorktreeTestProfileUnavailable) {
		t.Fatalf("group-readable profile accepted: %v", err)
	}
	if err := os.Chmod(profilePath, 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := readProjectWorktreeTestProfile(stateRoot)
	if err != nil || profile.ID != "x" || len(profile.argv) != 2 {
		t.Fatalf("canonical profile=%+v err=%v", profile, err)
	}
	if err := os.WriteFile(profilePath, []byte(`{"version":1,"profile_id":"x","argv":["sh","-c","caller"],"timeout_seconds":30,"cwd":"/tmp"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectWorktreeTestProfile(stateRoot); !errors.Is(err, ErrProjectWorktreeTestProfileUnavailable) {
		t.Fatalf("profile with caller-controlled cwd accepted: %v", err)
	}
	if !strings.HasPrefix(profile.Digest, "sha256:") {
		t.Fatalf("invalid operator profile digest: %q", profile.Digest)
	}
}
