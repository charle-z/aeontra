package workqueue

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskObjectiveContractReceiptAndMigration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	config := Config{Root: root, ControllerID: "objective-receipt"}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	spec := TaskSpec{
		IdempotencyKey: "objective-receipt-0001", Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
		TestAcceptanceContract: &TaskTestAcceptanceContract{Version: 1, ProfileID: "go-check", ProfileDigest: "sha256:" + strings.Repeat("c", 64)},
		ObjectiveContract:      &TaskObjectiveContract{Version: 1, MinimumCommitsAheadPerWorker: 1, MinimumChangedPathsPerWorker: 1, RequireClean: true},
	}
	task, _, err := store.CreateTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	changed := *spec.ObjectiveContract
	changed.RequireClean = false
	conflict := spec
	conflict.ObjectiveContract = &changed
	if _, _, err := store.CreateTask(conflict); err == nil {
		t.Fatal("changed objective contract reused the task")
	}
	worker, err := store.LeaseTaskWorker(task.ID, 0, "objective-holder-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = store.BindTaskWorker(TaskWorkerBinding{TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, WorktreeID: "wt_0123456789abcdef0123456789abcdef", WorkspaceID: "ws_0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteTaskWorker(task.ID, 0, worker.LeaseID, worker.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	testReceipt, err := store.RecordTaskWorkerTestReceipt(TaskTestAcceptanceReceipt{
		Version: 1, TaskID: task.ID, Ordinal: 0, JobID: worker.JobID, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID,
		BaseCommit: task.BaseCommit, HeadCommit: strings.Repeat("d", 40), Branch: "codex/worktree-0123456789abcdef0123456789abcdef",
		LeaseID: worker.LeaseID, Fence: worker.Fence, ContentDigest: "sha256:" + strings.Repeat("e", 64), ProfileID: spec.TestAcceptanceContract.ProfileID,
		ProfileDigest: spec.TestAcceptanceContract.ProfileDigest, ContractDigest: TaskTestAcceptanceContractDigest(spec.TestAcceptanceContract), EdgeOperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EdgeResult: TaskTestEdgeResultPassed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err == nil {
		t.Fatal("test receipt alone permitted objective cleanup")
	}
	task, _, err = store.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker = task.Workers[0]
	receipt := TaskObjectiveReceipt{Version: 1, TaskID: task.ID, Ordinal: 0, ContractDigest: TaskObjectiveContractDigest(task, worker), TestReceiptDigest: TaskTestReceiptDigest(testReceipt), HeadCommit: testReceipt.HeadCommit, Clean: true, CommitsAheadBase: 1, ChangedPathCount: 1}
	bad := receipt
	bad.ContractDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := store.RecordTaskWorkerObjective(bad); err == nil {
		t.Fatal("different goal contract accepted")
	}
	bad = receipt
	bad.TestReceiptDigest = "sha256:" + strings.Repeat("f", 64)
	if _, err := store.RecordTaskWorkerObjective(bad); err == nil {
		t.Fatal("different tested tree accepted")
	}
	recorded, err := store.RecordTaskWorkerObjective(receipt)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.RecordTaskWorkerObjective(receipt)
	if err != nil || replayed != recorded {
		t.Fatalf("immutable replay=%+v err=%v", replayed, err)
	}
	bad = receipt
	bad.ChangedPathCount++
	if _, err := store.RecordTaskWorkerObjective(bad); err == nil {
		t.Fatal("receipt was overwritten")
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, worker.LeaseID, worker.Fence); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(config)
	if err != nil {
		t.Fatal(err)
	}
	persisted, found, err := store.Task(task.ID)
	if err != nil || !found || !SameTaskObjectiveContract(persisted.ObjectiveContract, spec.ObjectiveContract) || persisted.Workers[0].ObjectiveReceipt == nil || *persisted.Workers[0].ObjectiveReceipt != recorded {
		t.Fatalf("receipt/contract lost on restart: %+v err=%v", persisted, err)
	}
	if _, err := store.db.Exec(`UPDATE task_groups SET objective_contract='{"version":1,"unknown":true}' WHERE task_id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Task(task.ID); err == nil {
		t.Fatal("unknown persisted contract field accepted")
	}
}

func TestTaskObjectiveCriteriaAreOptInAndBounded(t *testing.T) {
	if ValidateTaskObjectiveContract(nil) != nil {
		t.Fatal("manual review compatibility lost")
	}
	for _, contract := range []*TaskObjectiveContract{{Version: 2}, {Version: 1, MinimumCommitsAheadPerWorker: -1}, {Version: 1, MinimumChangedPathsPerWorker: 10001}} {
		if ValidateTaskObjectiveContract(contract) == nil {
			t.Fatalf("invalid contract accepted: %+v", contract)
		}
	}
	c := &TaskObjectiveContract{Version: 1}
	if !TaskObjectiveCriteriaSatisfied(c, false, 0, 0) {
		t.Fatal("read-only/dirty zero-change objective restricted")
	}
	c.RequireClean = true
	if TaskObjectiveCriteriaSatisfied(c, false, 0, 0) {
		t.Fatal("declared clean criterion ignored")
	}
}

func TestSchemaThreeMigrationPreservesLegacyTask(t *testing.T) {
	root := filepath.Join(t.TempDir(), "queue")
	config := Config{Root: root, ControllerID: "objective-migration"}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	spec := TaskSpec{IdempotencyKey: "objective-legacy-0001", Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"}, Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600}
	task, _, err := store.CreateTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`ALTER TABLE task_groups DROP COLUMN objective_contract`, `ALTER TABLE task_workers DROP COLUMN objective_receipt`, `PRAGMA user_version=3`} {
		if _, err := store.db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(config)
	if err != nil {
		t.Fatal(err)
	}
	persisted, found, err := store.Task(task.ID)
	if err != nil || !found || persisted.ObjectiveContract != nil || persisted.Workers[0].ObjectiveReceipt != nil || persisted.State != task.State {
		t.Fatalf("legacy task changed: %+v err=%v", persisted, err)
	}
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 4 {
		t.Fatalf("schema=%d err=%v", version, err)
	}
}
