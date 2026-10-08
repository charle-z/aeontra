package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

func TestIntegrationSourceLocksDoNotAliasTaskStartLocks(t *testing.T) {
	var server Server
	taskID := "tg_" + strings.Repeat("a", 32)
	source := server.projectTaskSourceLock(taskID)
	if source != server.projectTaskSourceLock(taskID) {
		t.Fatal("the same source must retain a stable lock")
	}
	for index := 0; index < 1024; index++ {
		start := server.projectTaskStartLock("start:integration-lock-check-" + strconv.Itoa(index))
		start.Lock()
		acquired := source.TryLock()
		if acquired {
			source.Unlock()
		}
		start.Unlock()
		if !acquired {
			t.Fatal("integration source lock aliases a held task-start lock")
		}
	}
}

type integrationTaskEdgeStore struct {
	*projectTaskEdgeStore
	legacyEvidence, ancestryUnavailable, wrongAncestryHEAD bool
}

func (s *integrationTaskEdgeStore) WaitOperation(ctx context.Context, id string, timeout time.Duration) (edge.Operation, error) {
	op, err := s.projectTaskEdgeStore.WaitOperation(ctx, id, timeout)
	if err != nil {
		return op, err
	}
	if op.Kind == edge.OperationProjectWorktreeStatus && len(op.Request.WorktreeAncestorCommits) != 0 {
		if s.ancestryUnavailable {
			op.State = edge.OperationFailed
		}
		if !s.legacyEvidence {
			op.Result.WorktreeAncestorCommits = append([]string(nil), op.Request.WorktreeAncestorCommits...)
			op.Result.WorktreeAncestorsVerified = true
		}
		if s.wrongAncestryHEAD {
			op.Result.WorktreeHeadCommit = strings.Repeat("f", 40)
		}
		s.mu.Lock()
		s.operations[id] = op
		s.mu.Unlock()
	}
	return op, nil
}

func integrationTaskFixture(t *testing.T) (*Server, *modelturn.Store, *workqueue.Store, *integrationTaskEdgeStore, projectTaskView, projectTaskStartParams) {
	t.Helper()
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "integration-tests"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := &integrationTaskEdgeStore{projectTaskEdgeStore: newProjectTaskEdgeStore()}
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Implement alpha independently.","Implement beta independently."],"timeout_seconds":600,"idempotency_key":"integration-source-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var source projectTaskView
	if err := json.Unmarshal([]byte(output), &source); err != nil {
		t.Fatal(err)
	}
	for i, w := range source.Workers {
		if err := turns.CompleteRuntime(context.Background(), w.RuntimeID); err != nil {
			t.Fatal(err)
		}
		edges.mu.Lock()
		r := edges.worktrees[w.WorktreeID]
		r.WorktreeEvidenceKnown = true
		r.WorktreeHeadCommit = strings.Repeat(string(rune('c'+i)), 40)
		r.WorktreeClean = true
		r.WorktreeCommitsAheadBase = 1
		r.WorktreeChangedPathCount = 1
		edges.worktrees[w.WorktreeID] = r
		edges.mu.Unlock()
	}
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	task, _, err := queue.Task(source.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	source = server.projectTaskStatusView(context.Background(), task)
	contract := &workqueue.TaskIntegrationContract{Version: 1, SourceTaskID: source.TaskID, ExpectedBaseCommit: source.BaseCommit, Workers: []workqueue.TaskIntegrationWorker{{Ordinal: 0, HeadCommit: source.Workers[0].HeadCommit}, {Ordinal: 1, HeadCommit: source.Workers[1].HeadCommit}}}
	params := projectTaskStartParams{Alias: "project", Target: "parrot", Goals: []string{"Review both changes and integrate the verified result."}, TimeoutSeconds: 600, IdempotencyKey: "integration-review-0001", TestProfileID: "go-check", ObjectiveContract: &workqueue.TaskObjectiveContract{Version: 1, RequireClean: true}, IntegrationContract: contract}
	return server, turns, queue, edges, source, params
}

func startIntegrationTestTask(t *testing.T, server *Server, params projectTaskStartParams) projectTaskView {
	t.Helper()
	args, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	output, err := server.table["project_task_start"].handler(args)
	if err != nil {
		t.Fatal(err)
	}
	var task projectTaskView
	if err := json.Unmarshal([]byte(output), &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func finishIntegrationTestTask(t *testing.T, server *Server, turns *modelturn.Store, edges *integrationTaskEdgeStore, task projectTaskView) {
	t.Helper()
	w := task.Workers[0]
	if err := turns.CompleteRuntime(context.Background(), w.RuntimeID); err != nil {
		t.Fatal(err)
	}
	edges.mu.Lock()
	r := edges.worktrees[w.WorktreeID]
	r.WorktreeEvidenceKnown = true
	r.WorktreeHeadCommit = strings.Repeat("e", 40)
	r.WorktreeClean = true
	r.WorktreeCommitsAheadBase = 3
	r.WorktreeChangedPathCount = 2
	edges.worktrees[w.WorktreeID] = r
	edges.mu.Unlock()
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(projectTaskTestParams{TaskID: task.TaskID, Ordinal: 0})
	if _, err := server.table["project_task_test_start"].handler(args); err != nil {
		t.Fatal(err)
	}
	edges.mu.Lock()
	edges.testProcessState = "exited"
	edges.testExitKnown = true
	edges.testExitCode = 0
	edges.mu.Unlock()
}

func TestProjectTaskIntegrationPinsIndependentReviewerAndBlocksSourceCleanup(t *testing.T) {
	server, _, queue, edges, source, params := integrationTaskFixture(t)
	started := startIntegrationTestTask(t, server, params)
	if len(started.Workers) != 1 || started.Workers[0].Role != "reviewer_integrator" || started.Workers[0].WorktreeID == source.Workers[0].WorktreeID || started.Workers[0].WorktreeID == source.Workers[1].WorktreeID {
		t.Fatalf("integrator=%+v", started)
	}
	persisted, _, err := queue.Task(started.TaskID)
	if err != nil || len(persisted.IntegrationPins) != 2 || persisted.IntegrationPins[0].HeadCommit != source.Workers[0].HeadCommit {
		t.Fatalf("pins=%+v err=%v", persisted, err)
	}
	replayed := startIntegrationTestTask(t, server, params)
	if replayed.TaskID != started.TaskID || replayed.Workers[0].RuntimeID != started.Workers[0].RuntimeID {
		t.Fatal("replay duplicated integration")
	}
	if len(edges.worktrees) != 3 {
		t.Fatalf("replay worktrees=%d", len(edges.worktrees))
	}
	cleanup, _ := json.Marshal(projectTaskCleanupParams{TaskID: source.TaskID, IdempotencyKey: "integration-source-cleanup-0001"})
	if _, err := server.table["project_task_cleanup"].handler(cleanup); err == nil {
		t.Fatal("source cleanup accepted while integrator requires it")
	}
	args, _ := json.Marshal(projectTaskIDParams{TaskID: started.TaskID})
	if _, err := server.table["project_task_cancel"].handler(args); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if required, err := queue.TaskRequiredByIntegration(source.TaskID); err != nil || required {
		t.Fatalf("cancel pin required=%v err=%v", required, err)
	}
}

func TestProjectTaskIntegrationRequiresFreshAncestryAndTests(t *testing.T) {
	server, turns, queue, edges, source, params := integrationTaskFixture(t)
	started := startIntegrationTestTask(t, server, params)
	finishIntegrationTestTask(t, server, turns, edges, started)
	status := func() projectTaskView {
		t.Helper()
		task, _, err := queue.Task(started.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		return server.projectTaskStatusView(context.Background(), task)
	}
	for _, mode := range []string{"old-edge", "missing-ancestry", "head-drift"} {
		edges.legacyEvidence = mode == "old-edge"
		edges.ancestryUnavailable = mode == "missing-ancestry"
		edges.wrongAncestryHEAD = mode == "head-drift"
		view := status()
		if view.Workers[0].AcceptanceState == "accepted" || view.IntegrationState == "verified" {
			t.Fatalf("%s accepted: %+v", mode, view)
		}
	}
	edges.legacyEvidence = false
	edges.ancestryUnavailable = false
	edges.wrongAncestryHEAD = false
	view := status()
	if view.Workers[0].AcceptanceState != "accepted" || view.IntegrationState != "verified" {
		t.Fatalf("verified candidate=%+v", view)
	}
	task, _, err := queue.Task(started.TaskID)
	if err != nil || task.IntegrationReceipt == nil {
		t.Fatalf("receipt=%+v err=%v", task.IntegrationReceipt, err)
	}
	edges.mu.Lock()
	edges.testStale = true
	edges.mu.Unlock()
	if stale := status(); stale.Workers[0].AcceptanceState == "accepted" {
		t.Fatal("stale tests retained integration acceptance")
	}
	edges.mu.Lock()
	edges.testStale = false
	r := edges.worktrees[source.Workers[0].WorktreeID]
	r.WorktreeHeadCommit = strings.Repeat("f", 40)
	edges.worktrees[source.Workers[0].WorktreeID] = r
	edges.mu.Unlock()
	if stale := status(); stale.IntegrationState != "stale" || stale.Workers[0].AcceptanceState == "accepted" {
		t.Fatalf("source drift retained acceptance: %+v", stale)
	}
	args, _ := json.Marshal(params)
	if _, err := server.table["project_task_start"].handler(args); err == nil {
		t.Fatal("stale source replay accepted")
	}
}

func TestProjectTaskIntegrationReceiptSurvivesVerifiedCleanup(t *testing.T) {
	server, turns, queue, edges, source, params := integrationTaskFixture(t)
	started := startIntegrationTestTask(t, server, params)
	finishIntegrationTestTask(t, server, turns, edges, started)
	task, _, _ := queue.Task(started.TaskID)
	view := server.projectTaskStatusView(context.Background(), task)
	if view.IntegrationState != "verified" {
		t.Fatalf("view=%+v", view)
	}
	cleanup, _ := json.Marshal(projectTaskCleanupParams{TaskID: started.TaskID, IdempotencyKey: "integration-review-cleanup-0001"})
	if output, err := server.table["project_task_cleanup"].handler(cleanup); err != nil || !strings.Contains(output, `"integration_state":"verified"`) {
		t.Fatalf("cleanup=%s err=%v", output, err)
	}
	if required, err := queue.TaskRequiredByIntegration(source.TaskID); err != nil || required {
		t.Fatalf("finished source pin required=%v err=%v", required, err)
	}
	cleanup, _ = json.Marshal(projectTaskCleanupParams{TaskID: source.TaskID, IdempotencyKey: "integration-source-cleanup-0001"})
	if _, err := server.table["project_task_cleanup"].handler(cleanup); err != nil {
		t.Fatal(err)
	}
	task, _, _ = queue.Task(started.TaskID)
	retained := server.projectTaskStatusView(context.Background(), task)
	if retained.IntegrationState != "verified" || retained.Workers[0].AcceptanceState != "accepted" || !reflect.DeepEqual(retained.IntegrationContract, params.IntegrationContract) {
		t.Fatalf("retained=%+v", retained)
	}
}

func TestProjectTaskIntegrationRejectsMissingCleanContractAndWrongBase(t *testing.T) {
	server, _, _, _, _, params := integrationTaskFixture(t)
	for _, mutate := range []func(*projectTaskStartParams){func(p *projectTaskStartParams) { p.ObjectiveContract = nil }, func(p *projectTaskStartParams) { p.TestProfileID = "" }, func(p *projectTaskStartParams) { p.Goals = append(p.Goals, "recursive worker") }, func(p *projectTaskStartParams) { p.IntegrationContract.ExpectedBaseCommit = strings.Repeat("f", 40) }, func(p *projectTaskStartParams) { p.IntegrationContract.Workers[1].Ordinal = 0 }} {
		bad := params
		contract := *params.IntegrationContract
		contract.Workers = append([]workqueue.TaskIntegrationWorker(nil), contract.Workers...)
		bad.IntegrationContract = &contract
		mutate(&bad)
		args, _ := json.Marshal(bad)
		if _, err := server.table["project_task_start"].handler(args); err == nil {
			t.Fatal("invalid integration started")
		}
	}
}
