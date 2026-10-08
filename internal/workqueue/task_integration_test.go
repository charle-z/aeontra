package workqueue

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSchemaKeepsDurableContractPinsAndReceipt(t *testing.T) {
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	rows, err := store.db.Query(`SELECT integration_contract,integration_pins,integration_receipt FROM task_groups`)
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 4 {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func integrationStoreFixture(t *testing.T) (*Store, TaskGroup, TaskSpec) {
	t.Helper()
	store := openTestStore(t, Config{ControllerID: "controller-a"})
	sourceSpec := TaskSpec{IdempotencyKey: "integration-source-0001", Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64), WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64), "sha256:" + strings.Repeat("2", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111", "mb_22222222222222222222222222222222"}, Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 2, ExecutionTimeoutSeconds: 600}
	source, _, err := store.CreateTask(sourceSpec)
	if err != nil {
		t.Fatal(err)
	}
	for ordinal := 0; ordinal < 2; ordinal++ {
		w, err := store.LeaseTaskWorker(source.ID, ordinal, "holder-a", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.BindTaskWorker(TaskWorkerBinding{TaskID: source.ID, Ordinal: ordinal, JobID: w.JobID, LeaseID: w.LeaseID, Fence: w.Fence, WorktreeID: fmt.Sprintf("wt_%032x", ordinal+1), WorkspaceID: fmt.Sprintf("ws_%032x", ordinal+1), RuntimeID: fmt.Sprintf("mr_%032x", ordinal+1)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CompleteTaskWorker(source.ID, ordinal, w.LeaseID, w.Fence, Result{Outcome: StateSucceeded, Summary: "source completed"}); err != nil {
			t.Fatal(err)
		}
	}
	source, _, err = store.Task(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := &TaskIntegrationContract{Version: 1, ExpectedBaseCommit: source.BaseCommit, SourceTaskID: source.ID, Workers: []TaskIntegrationWorker{{Ordinal: 0, HeadCommit: strings.Repeat("c", 40)}, {Ordinal: 1, HeadCommit: strings.Repeat("d", 40)}}}
	spec := sourceSpec
	spec.IdempotencyKey = "integration-review-0001"
	spec.WorkerCount = 1
	spec.WorkerGoalHashes = spec.WorkerGoalHashes[:1]
	spec.WorkerGoalRefs = spec.WorkerGoalRefs[:1]
	spec.ObjectiveContract = &TaskObjectiveContract{Version: 1, RequireClean: true}
	spec.TestAcceptanceContract = &TaskTestAcceptanceContract{Version: 1, ProfileID: "go-check", ProfileDigest: "sha256:" + strings.Repeat("e", 64)}
	spec.IntegrationContract = c
	for _, selected := range c.Workers {
		spec.IntegrationPins = append(spec.IntegrationPins, TaskIntegrationPin(source, selected.Ordinal, selected.HeadCommit))
	}
	return store, source, spec
}

func TestIntegrationPinsSurviveRestartAndBindReplayAndCleanup(t *testing.T) {
	store, source, spec := integrationStoreFixture(t)
	task, created, err := store.CreateTask(spec)
	if err != nil || !created {
		t.Fatalf("create=%v err=%v", created, err)
	}
	if repeated, created, err := store.CreateTask(spec); err != nil || created || repeated.ID != task.ID {
		t.Fatalf("replay=%+v created=%v err=%v", repeated, created, err)
	}
	bad := spec
	bad.IntegrationPins = append([]TaskIntegrationSourcePin(nil), spec.IntegrationPins...)
	bad.IntegrationPins[0].Fence++
	if _, _, err := store.CreateTask(bad); err == nil {
		t.Fatal("changed source identity replay accepted")
	}
	if required, err := store.TaskRequiredByIntegration(source.ID); err != nil || !required {
		t.Fatalf("required=%v err=%v", required, err)
	}
	w := source.Workers[0]
	if err := store.MarkTaskWorkerWorktreeCleaned(source.ID, 0, w.LeaseID, w.Fence); err == nil {
		t.Fatal("required source cleanup accepted")
	}
	config := store.config
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, found, err := reopened.Task(task.ID)
	if err != nil || !found || !SameTaskIntegrationContract(persisted.IntegrationContract, spec.IntegrationContract) || !reflect.DeepEqual(persisted.IntegrationPins, spec.IntegrationPins) {
		t.Fatalf("durable pins=%+v err=%v", persisted, err)
	}
	if _, err := reopened.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if required, err := reopened.TaskRequiredByIntegration(source.ID); err != nil || required {
		t.Fatalf("cancelled pin required=%v err=%v", required, err)
	}
	if err := reopened.MarkTaskWorkerWorktreeCleaned(source.ID, 0, w.LeaseID, w.Fence); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationRejectsRecursiveChangedOrUnboundSources(t *testing.T) {
	store, source, spec := integrationStoreFixture(t)
	for _, mutate := range []func(*TaskSpec){
		func(s *TaskSpec) { s.IntegrationPins[0].Fence++ },
		func(s *TaskSpec) { s.Target = "other" },
		func(s *TaskSpec) { s.ObjectiveContract = &TaskObjectiveContract{Version: 1, RequireClean: false} },
		func(s *TaskSpec) { s.IntegrationPins = nil },
		func(s *TaskSpec) { s.WorkerCount = 2 },
	} {
		bad := spec
		bad.IntegrationPins = append([]TaskIntegrationSourcePin(nil), spec.IntegrationPins...)
		mutate(&bad)
		if _, _, err := store.CreateTask(bad); err == nil {
			t.Fatal("invalid integration accepted")
		}
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(source.ID, 0, source.Workers[0].LeaseID, source.Workers[0].Fence); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateTask(spec); err == nil {
		t.Fatal("cleaned source accepted")
	}
	var omitted TaskIntegrationWorker
	if err := json.Unmarshal([]byte(`{"head_commit":"`+strings.Repeat("a", 40)+`"}`), &omitted); err == nil {
		t.Fatal("omitted ordinal accepted")
	}
	duplicate := *spec.IntegrationContract
	duplicate.Workers = append([]TaskIntegrationWorker(nil), duplicate.Workers...)
	duplicate.Workers[1].Ordinal = 0
	if ValidateTaskIntegrationContract(&duplicate) == nil {
		t.Fatal("duplicate worker accepted")
	}
}

func TestIntegrationReceiptRequiresObjectiveAndSurvivesRestart(t *testing.T) {
	store, source, spec := integrationStoreFixture(t)
	task, _, err := store.CreateTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	w, err := store.LeaseTaskWorker(task.ID, 0, "holder-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w, err = store.BindTaskWorker(TaskWorkerBinding{TaskID: task.ID, Ordinal: 0, JobID: w.JobID, LeaseID: w.LeaseID, Fence: w.Fence, WorktreeID: "wt_33333333333333333333333333333333", WorkspaceID: "ws_33333333333333333333333333333333", RuntimeID: "mr_33333333333333333333333333333333"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.CompleteTaskWorker(task.ID, 0, w.LeaseID, w.Fence, Result{Outcome: StateSucceeded, Summary: "review completed"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := TaskIntegrationReceipt{Version: 1, TaskID: task.ID, ContractDigest: TaskIntegrationDigest(task.IntegrationContract), SourcesDigest: TaskIntegrationDigest(task.IntegrationPins), HeadCommit: strings.Repeat("e", 40)}
	if _, err := store.RecordTaskIntegrationReceipt(receipt); err == nil {
		t.Fatal("integration accepted without objective tests")
	}
	testReceipt, err := store.RecordTaskWorkerTestReceipt(TaskTestAcceptanceReceipt{Version: 1, TaskID: task.ID, Ordinal: 0, JobID: w.JobID, WorktreeID: w.WorktreeID, WorkspaceID: w.WorkspaceID, BaseCommit: task.BaseCommit, HeadCommit: receipt.HeadCommit, Branch: "codex/worktree-33333333333333333333333333333333", LeaseID: w.LeaseID, Fence: w.Fence, ContentDigest: "sha256:" + strings.Repeat("b", 64), ProfileID: task.TestAcceptanceContract.ProfileID, ProfileDigest: task.TestAcceptanceContract.ProfileDigest, ContractDigest: TaskTestAcceptanceContractDigest(task.TestAcceptanceContract), EdgeOperationID: "eo_33333333333333333333333333333333", EdgeResult: TaskTestEdgeResultPassed})
	if err != nil {
		t.Fatal(err)
	}
	objective, err := store.RecordTaskWorkerObjective(TaskObjectiveReceipt{Version: 1, TaskID: task.ID, Ordinal: 0, ContractDigest: TaskObjectiveContractDigest(task, w), TestReceiptDigest: TaskTestReceiptDigest(testReceipt), HeadCommit: receipt.HeadCommit, Clean: true, CommitsAheadBase: 1, ChangedPathCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, w.LeaseID, w.Fence); err == nil {
		t.Fatal("integration cleaned without ancestry receipt")
	}
	receipt.ObjectiveReceiptDigest = TaskIntegrationDigest(objective)
	recorded, err := store.RecordTaskIntegrationReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.RecordTaskIntegrationReceipt(receipt)
	if err != nil || replayed != recorded {
		t.Fatalf("receipt replay=%+v err=%v", replayed, err)
	}
	bad := receipt
	bad.HeadCommit = strings.Repeat("f", 40)
	if _, err := store.RecordTaskIntegrationReceipt(bad); err == nil {
		t.Fatal("receipt head replaced")
	}
	if err := store.MarkTaskWorkerWorktreeCleaned(task.ID, 0, w.LeaseID, w.Fence); err != nil {
		t.Fatal(err)
	}
	if required, err := store.TaskRequiredByIntegration(source.ID); err != nil || required {
		t.Fatalf("verified cleanup pin required=%v err=%v", required, err)
	}
	config := store.config
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, found, err := reopened.Task(task.ID)
	if err != nil || !found || persisted.IntegrationReceipt == nil || *persisted.IntegrationReceipt != recorded {
		t.Fatalf("receipt after restart=%+v err=%v", persisted.IntegrationReceipt, err)
	}
}
