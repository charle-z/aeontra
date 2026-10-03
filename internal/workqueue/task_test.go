package workqueue

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSchemaOneMigratesToDurableTaskGroupsAndObjectives(t *testing.T) {
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
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
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

func TestTaskAcceptanceContractIsDurableAndIdempotencyBound(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store, err := Open(Config{Root: root, ControllerID: "controller-acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	spec := TaskSpec{
		IdempotencyKey: "task-acceptance-0123", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		AcceptanceContract: &TaskAcceptanceContract{Version: 1, MinimumCommitsAheadPerWorker: 2, MinimumChangedPathsPerWorker: 3},
	}
	task, created, err := store.CreateTask(spec)
	if err != nil || !created || task.AcceptanceContract == nil || *task.AcceptanceContract != *spec.AcceptanceContract {
		t.Fatalf("task=%+v created=%v err=%v", task, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, ControllerID: "controller-acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	persisted, found, err := store.Task(task.ID)
	if err != nil || !found || persisted.AcceptanceContract == nil || *persisted.AcceptanceContract != *spec.AcceptanceContract {
		t.Fatalf("persisted=%+v found=%v err=%v", persisted, found, err)
	}
	replayed, created, err := store.CreateTask(spec)
	if err != nil || created || replayed.ID != task.ID {
		t.Fatalf("replay=%+v created=%v err=%v", replayed, created, err)
	}
	conflicting := spec
	changedContract := *spec.AcceptanceContract
	changedContract.MinimumChangedPathsPerWorker++
	conflicting.AcceptanceContract = &changedContract
	if _, _, err := store.CreateTask(conflicting); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed acceptance contract error=%v", err)
	}
}

func TestSchemaThreeMigrationPreservesLegacyTasksWithoutAcceptanceContract(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store, err := Open(Config{Root: root, ControllerID: "controller-migration"})
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-migration-0123", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE task_groups DROP COLUMN acceptance_contract_version`,
		`ALTER TABLE task_groups DROP COLUMN acceptance_min_commits_ahead`,
		`ALTER TABLE task_groups DROP COLUMN acceptance_min_changed_paths`,
		`ALTER TABLE task_groups DROP COLUMN test_profile_id`,
		`ALTER TABLE task_groups DROP COLUMN test_profile_digest`,
		`ALTER TABLE task_workers DROP COLUMN acceptance_receipt`,
		`ALTER TABLE task_workers DROP COLUMN worktree_cleaned`,
		`ALTER TABLE task_workers DROP COLUMN test_acceptance_receipt`,
		`DROP TABLE development_objectives`,
		`PRAGMA user_version=2`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatalf("prepare v2 database with %q: %v", statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, ControllerID: "controller-migration"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	var ignoredExtension string
	if err := store.db.QueryRow(`SELECT task_id FROM task_groups WHERE task_id=?`, task.ID).Scan(&ignoredExtension); err != nil || ignoredExtension != task.ID {
		t.Fatalf("v2-compatible task lookup=%q err=%v", ignoredExtension, err)
	}
	var objectiveTable int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='development_objectives'`).Scan(&objectiveTable); err != nil || objectiveTable != 1 {
		t.Fatalf("development objective table count=%d err=%v", objectiveTable, err)
	}
	preserved, found, err := store.Task(task.ID)
	if err != nil || !found || preserved.IdempotencyKey != task.IdempotencyKey || preserved.AcceptanceContract != nil || len(preserved.Workers) != 1 {
		t.Fatalf("preserved=%+v found=%v err=%v", preserved, found, err)
	}
}

func TestSchemaThreeExtensionMigrationIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`CREATE TABLE task_groups(task_id TEXT PRIMARY KEY) WITHOUT ROWID`,
		`CREATE VIEW task_workers AS SELECT 1 AS task_id`,
		`PRAGMA user_version=2`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureSchemaExtensions(db, 2); err == nil {
		t.Fatal("migration unexpectedly altered a view as a worker table")
	}
	rows, err := db.Query(`PRAGMA table_info(task_groups)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "acceptance_") || name == "test_profile_id" || name == "test_profile_digest" {
			t.Fatalf("partial migration left column %q behind", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	var objectiveTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='development_objectives'`).Scan(&objectiveTable); err != nil || objectiveTable != 0 {
		t.Fatalf("failed migration left objective table count=%d err=%v", objectiveTable, err)
	}
}

func TestTaskAcceptanceReceiptIsDurableImmutableAndCleanupGated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store, err := Open(Config{Root: root, ControllerID: "controller-receipt"})
	if err != nil {
		t.Fatal(err)
	}
	contract := &TaskAcceptanceContract{Version: 1, MinimumCommitsAheadPerWorker: 1, MinimumChangedPathsPerWorker: 1}
	task, _, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-receipt-0123", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		AcceptanceContract: contract,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = store.BindTaskWorker(TaskWorkerBinding{TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence,
		WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "worker completed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err == nil {
		t.Fatal("completed acceptance task was cleaned before its receipt")
	}
	receipt := TaskAcceptanceReceipt{Version: 1, TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID,
		WorktreeRole: "writer", BaseCommit: task.BaseCommit, HeadCommit: strings.Repeat("1", 40), Branch: "codex/worktree-0123456789abcdef0123456789abcdef",
		LeaseID: worker.LeaseID, Fence: worker.Fence, ContractDigest: TaskAcceptanceContractDigest(contract), Clean: true, CommitsAheadBase: 1, ChangedPathCount: 1}
	recorded, err := store.RecordTaskWorkerAcceptance(receipt)
	if err != nil || recorded.RecordedAt.IsZero() {
		t.Fatalf("receipt=%+v err=%v", recorded, err)
	}
	replayed, err := store.RecordTaskWorkerAcceptance(receipt)
	if err != nil || !replayed.RecordedAt.Equal(recorded.RecordedAt) {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	conflicting := receipt
	conflicting.ChangedPathCount++
	if _, err := store.RecordTaskWorkerAcceptance(conflicting); err == nil {
		t.Fatal("conflicting receipt replaced the original checkpoint")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, ControllerID: "controller-receipt"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	persisted, found, err := store.Task(task.ID)
	if err != nil || !found || persisted.Workers[0].AcceptanceReceipt == nil || !persisted.Workers[0].AcceptanceReceipt.RecordedAt.Equal(recorded.RecordedAt) {
		t.Fatalf("persisted=%+v found=%v err=%v", persisted, found, err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, "wl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", worker.Fence); err == nil {
		t.Fatal("stale lease replay was accepted for an already-cleaned worker")
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err != nil {
		t.Fatalf("cleanup marker replay: %v", err)
	}
	cleaned, found, err := store.Task(task.ID)
	if err != nil || !found || !cleaned.Workers[0].WorktreeCleaned || cleaned.Workers[0].AcceptanceReceipt == nil {
		t.Fatalf("cleaned=%+v found=%v err=%v", cleaned, found, err)
	}
}

func TestTaskAcceptanceContractRejectsUnknownOrUnboundedCriteria(t *testing.T) {
	for _, contract := range []*TaskAcceptanceContract{
		{Version: 2, MinimumCommitsAheadPerWorker: 1, MinimumChangedPathsPerWorker: 1},
		{Version: 1, MinimumCommitsAheadPerWorker: 0, MinimumChangedPathsPerWorker: 1},
		{Version: 1, MinimumCommitsAheadPerWorker: 1, MinimumChangedPathsPerWorker: 10001},
	} {
		if err := ValidateTaskAcceptanceContract(contract); err == nil {
			t.Errorf("invalid contract accepted: %+v", contract)
		}
	}
	if err := ValidateTaskAcceptanceContract(nil); err != nil {
		t.Fatalf("nil contract must retain manual-review compatibility: %v", err)
	}
}

func TestTaskTestAcceptanceReceiptIsDurableImmutableAndContractBound(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	store, err := Open(Config{Root: root, ControllerID: "controller-test-receipt"})
	if err != nil {
		t.Fatal(err)
	}
	contract := &TaskTestAcceptanceContract{Version: 1, ProfileID: "go.unit", ProfileDigest: "sha256:" + strings.Repeat("c", 64)}
	task, created, err := store.CreateTask(TaskSpec{
		IdempotencyKey: "task-test-receipt-0123", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		TestAcceptanceContract: contract,
	})
	if err != nil || !created || task.TestAcceptanceContract == nil || *task.TestAcceptanceContract != *contract {
		t.Fatalf("task=%+v created=%v err=%v", task, created, err)
	}
	var contractDiscriminator int
	if err := store.db.QueryRow(`SELECT acceptance_contract_version FROM task_groups WHERE task_id=?`, task.ID).Scan(&contractDiscriminator); err != nil || contractDiscriminator != 2 {
		t.Fatalf("test-contract legacy fail-closed discriminator=%d err=%v", contractDiscriminator, err)
	}
	conflictingSpec := TaskSpec{
		IdempotencyKey: "task-test-receipt-0123", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		TestAcceptanceContract: &TaskTestAcceptanceContract{Version: 1, ProfileID: "go.unit", ProfileDigest: "sha256:" + strings.Repeat("d", 64)},
	}
	if _, _, err := store.CreateTask(conflictingSpec); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed profile digest error=%v", err)
	}
	worker, err := store.LeaseTaskWorker(task.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = store.BindTaskWorker(TaskWorkerBinding{TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence,
		WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "worker completed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err == nil {
		t.Fatal("test-contract worktree was cleaned before recording its content digest and result")
	}
	receipt := TaskTestAcceptanceReceipt{
		Version: 1, TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID,
		BaseCommit: task.BaseCommit, HeadCommit: strings.Repeat("d", 40), Branch: "codex/worktree-0123456789abcdef0123456789abcdef",
		LeaseID: worker.LeaseID, Fence: worker.Fence, ContentDigest: "sha256:" + strings.Repeat("e", 64),
		ProfileID: contract.ProfileID, ProfileDigest: contract.ProfileDigest, ContractDigest: TaskTestAcceptanceContractDigest(contract),
		EdgeOperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EdgeResult: TaskTestEdgeResultPassed,
	}
	invalidReceipt := receipt
	invalidReceipt.Fence++
	if _, err := store.RecordTaskWorkerTestReceipt(invalidReceipt); err == nil {
		t.Fatal("stale fence was accepted for a test receipt")
	}
	failedReceipt := receipt
	failedReceipt.EdgeResult = "failed"
	if _, err := store.RecordTaskWorkerTestReceipt(failedReceipt); err == nil {
		t.Fatal("a failed test became the final acceptance receipt")
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err == nil {
		t.Fatal("failed tests allowed cleanup before a passing rerun")
	}
	recorded, err := store.RecordTaskWorkerTestReceipt(receipt)
	if err != nil || recorded.RecordedAt.IsZero() {
		t.Fatalf("receipt=%+v err=%v", recorded, err)
	}
	replayed, err := store.RecordTaskWorkerTestReceipt(receipt)
	if err != nil || !replayed.RecordedAt.Equal(recorded.RecordedAt) {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	conflicting := receipt
	conflicting.ContentDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := store.RecordTaskWorkerTestReceipt(conflicting); err == nil {
		t.Fatal("conflicting content digest replaced the original receipt")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, ControllerID: "controller-test-receipt"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	persisted, found, err := store.Task(task.ID)
	if err != nil || !found || persisted.TestAcceptanceContract == nil || *persisted.TestAcceptanceContract != *contract ||
		persisted.Workers[0].TestAcceptanceReceipt == nil || !persisted.Workers[0].TestAcceptanceReceipt.RecordedAt.Equal(recorded.RecordedAt) {
		t.Fatalf("persisted=%+v found=%v err=%v", persisted, found, err)
	}
	if persisted.State != TaskCompleted || persisted.Workers[0].State != StateSucceeded {
		t.Fatalf("a test receipt changed task lifecycle: %+v", persisted)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err != nil {
		t.Fatalf("cleanup after recording the test receipt: %v", err)
	}
}

func TestTaskTestAcceptanceContractIsVersionedProfileReferenceOnly(t *testing.T) {
	for _, contract := range []*TaskTestAcceptanceContract{
		{Version: 2, ProfileID: "go.unit", ProfileDigest: "sha256:" + strings.Repeat("a", 64)},
		{Version: 1, ProfileID: "go.unit", ProfileDigest: ""},
		{Version: 1, ProfileID: "go test ./...", ProfileDigest: "sha256:" + strings.Repeat("a", 64)},
		{Version: 1, ProfileID: "go.unit", ProfileDigest: "sha256:" + strings.Repeat("g", 64)},
	} {
		if err := ValidateTaskTestAcceptanceContract(contract); err == nil {
			t.Errorf("invalid test contract accepted: %+v", contract)
		}
	}
	if err := ValidateTaskTestAcceptanceContract(nil); err != nil {
		t.Fatalf("nil contract must retain legacy compatibility: %v", err)
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

func TestRecentProjectTasksRetainsCompletedWorkForChatRecovery(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	newTask := func(key, project, target, goalRef string) TaskGroup {
		t.Helper()
		task, _, err := store.CreateTask(TaskSpec{
			IdempotencyKey: key, Project: project, Target: target, BaseCommit: strings.Repeat("a", 40),
			GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{goalRef},
			Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	completed := newTask("recover-completed-01", "project", "parrot", "mb_11111111111111111111111111111111")
	worker, err := store.LeaseTaskWorker(completed.ID, 0, "worker-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(completed.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	active := newTask("recover-active-00001", "project", "parrot", "mb_22222222222222222222222222222222")
	other := newTask("recover-unrelated-001", "other", "parrot", "mb_33333333333333333333333333333333")
	tasks, err := store.RecentProjectTasks("project", "parrot", 10)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	seen := map[string]TaskState{}
	for _, task := range tasks {
		seen[task.ID] = task.State
		if task.ID == other.ID {
			t.Fatal("unrelated project entered recovery list")
		}
	}
	if seen[completed.ID] != TaskCompleted || seen[active.ID] == "" {
		t.Fatalf("recovery list omitted terminal or active work: %+v", seen)
	}
	if _, err := store.RecentProjectTasks("project", "parrot", MaxListResults+1); err == nil {
		t.Fatal("unbounded recovery list accepted")
	}
}
