package workqueue

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSchemaOneMigratesToDurableTaskGroups(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`PRAGMA user_version=1`,
		`CREATE TABLE queue_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE jobs(job_id TEXT PRIMARY KEY,idempotency_key TEXT NOT NULL UNIQUE,workspace TEXT NOT NULL,pool TEXT NOT NULL,profile TEXT NOT NULL,payload_hash TEXT NOT NULL,state TEXT NOT NULL,reason TEXT NOT NULL DEFAULT '',created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,cancel_requested INTEGER NOT NULL DEFAULT 0,attempt INTEGER NOT NULL DEFAULT 0,fence INTEGER NOT NULL DEFAULT 0,lease_id TEXT,lease_holder TEXT,lease_until INTEGER,outcome TEXT,summary TEXT,result_ref TEXT) WITHOUT ROWID`,
		`CREATE TABLE dependencies(job_id TEXT NOT NULL,dependency_id TEXT NOT NULL,PRIMARY KEY(job_id,dependency_id),FOREIGN KEY(job_id) REFERENCES jobs(job_id) ON DELETE CASCADE,FOREIGN KEY(dependency_id) REFERENCES jobs(job_id)) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(Config{Root: root, ControllerID: "controller-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	tasks, err := store.Tasks(10)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("tasks=%v err=%v", tasks, err)
	}
}

func TestTaskGroupBindsWorkersToFencedWorktreesAndRuntimes(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	spec := TaskSpec{
		IdempotencyKey: "task-create-01234567", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64), "sha256:" + strings.Repeat("2", 64), "sha256:" + strings.Repeat("3", 64)},
		WorkerGoalRefs:   []string{"mb_11111111111111111111111111111111", "mb_22222222222222222222222222222222", "mb_33333333333333333333333333333333"},
		Pool:             "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 3, ExecutionTimeoutSeconds: 600,
	}
	task, created, err := store.CreateTask(spec)
	if err != nil || !created || !taskIDPattern.MatchString(task.ID) || task.State != TaskQueued || len(task.Workers) != 3 {
		t.Fatalf("task=%+v created=%v err=%v", task, created, err)
	}
	repeated, created, err := store.CreateTask(spec)
	if err != nil || created || repeated.ID != task.ID {
		t.Fatalf("repeat=%+v created=%v err=%v", repeated, created, err)
	}
	conflict := spec
	conflict.WorkerCount = 2
	if _, _, err := store.CreateTask(conflict); err == nil {
		t.Fatal("task idempotency conflict accepted")
	}

	worker, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil || worker.Fence != 1 || !leaseIDPattern.MatchString(worker.LeaseID) {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
	if _, err := store.BindTaskWorkerOperation(TaskWorkerOperationBinding{TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindTaskWorker(TaskWorkerBinding{
		TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence,
		WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef",
	})
	if err != nil || bound.WorktreeID == "" || bound.RuntimeID != "" {
		t.Fatalf("bound=%+v err=%v", bound, err)
	}
	bound, err = store.BindTaskWorker(TaskWorkerBinding{
		TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence,
		WorktreeID: bound.WorktreeID, WorkspaceID: bound.WorkspaceID, RuntimeID: "mr_0123456789abcdef0123456789abcdef",
	})
	if err != nil || bound.RuntimeID == "" {
		t.Fatalf("runtime bound=%+v err=%v", bound, err)
	}
	idempotent, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil || idempotent.WorktreeID != bound.WorktreeID || idempotent.WorkspaceID != bound.WorkspaceID || idempotent.RuntimeID != bound.RuntimeID {
		t.Fatalf("idempotent lease lost durable binding: worker=%+v err=%v", idempotent, err)
	}
	stale := TaskWorkerBinding{
		TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: "wl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Fence: worker.Fence,
		WorktreeID: bound.WorktreeID, WorkspaceID: bound.WorkspaceID, RuntimeID: bound.RuntimeID,
	}
	if _, err := store.BindTaskWorker(stale); err == nil {
		t.Fatal("stale worker binding accepted")
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "worker completed"}); err != nil {
		t.Fatal(err)
	}
	status, found, err := store.Task(task.ID)
	if err != nil || !found || status.State != TaskRunning || status.Workers[0].State != StateSucceeded {
		t.Fatalf("status=%+v found=%v err=%v", status, found, err)
	}
}

func TestTaskGroupCancellationPreservesTerminalWorkers(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	task, _, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-cancel-012345", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64), "sha256:" + strings.Repeat("2", 64)},
		WorkerGoalRefs:   []string{"mb_11111111111111111111111111111111", "mb_22222222222222222222222222222222"},
		Pool:             "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 2, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.CancelTask(task.ID)
	if err != nil || cancelled.State != TaskCancelling || !cancelled.Workers[0].CancelRequested || cancelled.Workers[1].State != StateCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateCancelled, Summary: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	terminal, _, err := store.Task(task.ID)
	if err != nil || terminal.State != TaskCancelled {
		t.Fatalf("terminal=%+v err=%v", terminal, err)
	}
}

func TestTaskWorkerLeaseExpiryPreservesBindingsAndAdvancesFence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	store.now = func() time.Time { return now }
	task, _, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-recover-012345", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", MinLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTaskWorkerOperation(TaskWorkerOperationBinding{TaskID: task.ID, Ordinal: 0, JobID: first.JobID, LeaseID: first.LeaseID, Fence: first.Fence, OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTaskWorker(TaskWorkerBinding{TaskID: task.ID, Ordinal: 0, JobID: first.JobID, LeaseID: first.LeaseID, Fence: first.Fence, WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef", RuntimeID: "mr_0123456789abcdef0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL + time.Second)
	if err := store.RecoverExpired(); err != nil {
		t.Fatal(err)
	}
	queued, _, err := store.Task(task.ID)
	if err != nil || queued.Workers[0].State != StateQueued || queued.Workers[0].OperationID == "" || queued.Workers[0].RuntimeID == "" {
		t.Fatalf("recovered=%+v err=%v", queued, err)
	}
	second, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0002", time.Minute)
	if err != nil || second.Fence != first.Fence+1 || second.LeaseID == first.LeaseID || second.WorktreeID != "wt_0123456789abcdef0123456789abcdef" || second.WorkspaceID != "ws_0123456789abcdef0123456789abcdef" || second.RuntimeID != "mr_0123456789abcdef0123456789abcdef" {
		t.Fatalf("first=%+v second=%+v err=%v", first, second, err)
	}
	recovered, _, err := store.Task(task.ID)
	worker := recovered.Workers[0]
	if err != nil || worker.OperationID == "" || worker.WorktreeID == "" || worker.WorkspaceID == "" || worker.RuntimeID == "" || worker.Fence != second.Fence {
		t.Fatalf("worker=%+v err=%v", worker, err)
	}
}

func TestGenericCleanupPreservesDurableTaskEvidence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	store.now = func() time.Time { return now }
	task, _, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-retain-0123456", Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40),
		GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(48 * time.Hour)
	removed, err := store.CleanupTerminal(24*time.Hour, 10)
	if err != nil || removed != 0 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	retained, found, err := store.Task(task.ID)
	if err != nil || !found || retained.State != TaskCompleted {
		t.Fatalf("retained=%+v found=%v err=%v", retained, found, err)
	}
}

func TestTerminalTaskWorkersDoNotConsumeQueueBounds(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a", MaxJobs: 1, MaxJobsPerWorkspace: 1})
	makeSpec := func(key, hash, ref string) TaskSpec {
		return TaskSpec{
			IdempotencyKey: key, Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40),
			GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{hash}, WorkerGoalRefs: []string{ref},
			Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		}
	}
	first, _, err := store.CreateTask(makeSpec("terminal-task-bound-1", "sha256:"+strings.Repeat("1", 64), "mb_11111111111111111111111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.LeaseTaskWorker(first.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(first.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateTask(makeSpec("terminal-task-bound-2", "sha256:"+strings.Repeat("2", 64), "mb_22222222222222222222222222222222")); err != nil {
		t.Fatalf("terminal task worker consumed queue bound: %v", err)
	}
	if err := store.Integrity(); err != nil {
		t.Fatalf("terminal task evidence exceeded integrity bound: %v", err)
	}
}

func TestTaskReconciliationListExcludesRetainedTerminalGroups(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	newTask := func(key, workerHash, goalRef string) TaskGroup {
		t.Helper()
		task, _, err := store.CreateTask(TaskSpec{
			IdempotencyKey: key, Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40),
			GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{workerHash}, WorkerGoalRefs: []string{goalRef},
			Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	terminal := newTask("task-terminal-012345", "sha256:"+strings.Repeat("1", 64), "mb_11111111111111111111111111111111")
	worker, err := store.LeaseTaskWorker(terminal.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(terminal.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	active := newTask("task-active-01234567", "sha256:"+strings.Repeat("2", 64), "mb_22222222222222222222222222222222")
	tasks, err := store.Tasks(1)
	if err != nil || len(tasks) != 1 || tasks[0].ID != active.ID {
		t.Fatalf("tasks=%+v active=%s err=%v", tasks, active.ID, err)
	}
	retained, found, err := store.Task(terminal.ID)
	if err != nil || !found || retained.State != TaskCompleted {
		t.Fatalf("retained=%+v found=%v err=%v", retained, found, err)
	}
}
