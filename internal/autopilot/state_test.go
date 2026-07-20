package autopilot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDurableJobSurvivesRestartPauseResumeAndCancel(t *testing.T) {
	workspace := privateWorkspace(t)
	now := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	store := Store{Workspace: workspace, Now: func() time.Time { return now }}
	job, created, err := store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
	if err != nil || !created || job.State != StateRunning {
		t.Fatalf("start=%+v created=%t err=%v", job, created, err)
	}
	reopened := Store{Workspace: workspace, Now: func() time.Time { return now }}
	same, created, err := reopened.Start(job.WorkspaceID, RunUntilCompletedOrCancelled)
	if err != nil || created || same.JobID != job.JobID {
		t.Fatalf("restart=%+v created=%t err=%v", same, created, err)
	}
	paused, err := reopened.Pause()
	if err != nil || paused.State != StatePaused {
		t.Fatalf("pause=%+v err=%v", paused, err)
	}
	resumed, err := reopened.Resume()
	if err != nil || resumed.State != StateRunning || resumed.JobID != job.JobID {
		t.Fatalf("resume=%+v err=%v", resumed, err)
	}
	cancelled, err := reopened.Cancel()
	if err != nil || cancelled.State != StateCancelled {
		t.Fatalf("cancel=%+v err=%v", cancelled, err)
	}
	info, err := os.Stat(filepath.Join(workspace, ".mcp-devbox", StateFile))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("state mode=%v err=%v", info, err)
	}
}

func TestCyclesContinueBeyondOneHourAndPersistProgress(t *testing.T) {
	workspace := privateWorkspace(t)
	now := time.Date(2026, 7, 20, 1, 0, 0, 0, time.UTC)
	store := Store{Workspace: workspace, Now: func() time.Time { return now }}
	job, _, _ := store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
	for i := 0; i < 5; i++ {
		now = now.Add(20 * time.Minute)
		var err error
		job, err = store.RecordCycle(CycleResult{Progress: true, CheckpointChanged: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if job.State != StateRunning || job.ProgressRevision != 5 || job.CycleCount != 5 || now.Sub(job.CreatedAt) <= time.Hour {
		t.Fatalf("job=%+v elapsed=%v", job, now.Sub(job.CreatedAt))
	}
	loaded, err := store.Load()
	if err != nil || loaded.ProgressRevision != 5 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestCircuitBreakersStopNoProgressAndRepeatedFailures(t *testing.T) {
	workspace := privateWorkspace(t)
	store := Store{Workspace: workspace}
	_, _, _ = store.Start("ws_0123456789abcdef0123456789abcdef", RunUntilCompletedOrCancelled)
	first, err := store.RecordCycle(CycleResult{})
	if err != nil || first.State != StateRunning {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := store.RecordCycle(CycleResult{})
	if err != nil || second.State != StateBlocked || second.SafeCode != "no_progress" {
		t.Fatalf("second=%+v err=%v", second, err)
	}

	workspace = privateWorkspace(t)
	store = Store{Workspace: workspace}
	_, _, _ = store.Start("ws_11111111111111111111111111111111", RunUntilCompletedOrCancelled)
	for i := 0; i < 3; i++ {
		job, err := store.RecordCycle(CycleResult{Progress: true, FailureCode: "provider_transient"})
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && job.State != StateRunning {
			t.Fatalf("early block=%+v", job)
		}
		if i == 2 && (job.State != StateBlocked || job.SafeCode != "repeated_failure") {
			t.Fatalf("missing breaker=%+v", job)
		}
	}
}

func privateWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".mcp-devbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	return workspace
}
