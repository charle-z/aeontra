package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type projectTaskEdgeStore struct {
	mu         sync.Mutex
	next       int
	operations map[string]edge.Operation
	workspaces map[string]edge.WorkspaceBinding
	worktrees  map[string]edge.OperationResult
}

type inactiveProjectTaskEdgeStore struct{ *projectTaskEdgeStore }

func (*inactiveProjectTaskEdgeStore) DeviceActive(string) bool { return false }

func newProjectTaskEdgeStore() *projectTaskEdgeStore {
	return &projectTaskEdgeStore{operations: map[string]edge.Operation{}, workspaces: map[string]edge.WorkspaceBinding{}, worktrees: map[string]edge.OperationResult{}}
}

func (*projectTaskEdgeStore) DeviceActive(string) bool { return true }

func (*projectTaskEdgeStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	return edge.Device{ID: testEdgeDeviceID, Name: name, State: edge.StateActive}, nil
}

func (s *projectTaskEdgeStore) ResolveWorkspace(id string) (edge.WorkspaceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.workspaces[id]
	if !ok {
		return edge.WorkspaceBinding{}, fmt.Errorf("workspace not found")
	}
	return binding, nil
}

func (s *projectTaskEdgeStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.operations {
		if existing.DeviceID == deviceID && existing.Kind == kind && reflect.DeepEqual(existing.Request, request) {
			return existing, false, nil
		}
	}
	s.next++
	id := fmt.Sprintf("eo_%032x", s.next)
	op := edge.Operation{ID: id, DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}
	s.operations[id] = op
	return op, true, nil
}

func (s *projectTaskEdgeStore) WaitOperation(_ context.Context, id string, _ time.Duration) (edge.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.operations[id]
	op.State = edge.OperationSucceeded
	result := edge.OperationResult{
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	}
	switch op.Kind {
	case edge.OperationProjectSnapshot:
		result.WorkspaceID = "ws_11111111111111111111111111111111"
		result.SnapshotBranch = "main"
		result.SnapshotHead = "0123456789abcdef0123456789abcdef01234567"
		result.SnapshotClean = true
	case edge.OperationProjectWorktreeCreate:
		parsed, _ := strconv.ParseUint(strings.TrimPrefix(id, "eo_"), 16, 64)
		index := int(parsed)
		if index < 1 {
			index = 1
		}
		result.WorktreeID = fmt.Sprintf("wt_%032x", index)
		result.WorkspaceID = fmt.Sprintf("ws_%032x", index+100)
		result.WorktreeState = "ready"
		result.WorktreeRole = op.Request.WorktreeRole
		if result.WorktreeRole == "" {
			result.WorktreeRole = "writer"
		}
		result.WorktreeBaseCommit = "0123456789abcdef0123456789abcdef01234567"
		result.WorktreeBranch = fmt.Sprintf("codex/worktree-%032x", index)
		result.WorkJobID, result.WorkLeaseID, result.WorkFence = op.Request.WorkJobID, op.Request.WorkLeaseID, op.Request.WorkFence
		result.WorktreeCreatedAt = time.Unix(1, 0).UTC().Format(time.RFC3339Nano)
		result.WorktreeUpdatedAt = result.WorktreeCreatedAt
		s.workspaces[result.WorkspaceID] = edge.WorkspaceBinding{WorkspaceID: result.WorkspaceID, DeviceID: testEdgeDeviceID, Profile: "linux-workcell", Mode: "dev"}
		s.worktrees[result.WorktreeID] = result
	case edge.OperationProjectWorktreeClaim:
		result = s.worktrees[op.Request.WorktreeID]
		result.WorkLeaseID, result.WorkFence = op.Request.WorkLeaseID, op.Request.WorkFence
		result.WorktreeUpdatedAt = time.Unix(2, 0).UTC().Format(time.RFC3339Nano)
		s.worktrees[result.WorktreeID] = result
	case edge.OperationProjectWorktreeStatus:
		result = s.worktrees[op.Request.WorktreeID]
		result.WorktreeEvidenceKnown = true
		result.WorktreeHeadCommit = "1123456789abcdef0123456789abcdef01234567"
		result.WorktreeClean = true
		result.WorktreeCommitsAheadBase = 1
		result.WorktreeChangedPathCount = 1
	case edge.OperationProjectWorktreeCleanup:
		result = s.worktrees[op.Request.WorktreeID]
		result.WorktreeState = "removed"
		result.WorktreeUpdatedAt = time.Unix(3, 0).UTC().Format(time.RFC3339Nano)
		s.worktrees[result.WorktreeID] = result
	}
	op.Result = result
	s.operations[id] = op
	return op, nil
}

func (s *projectTaskEdgeStore) OperationStatus(id string) (edge.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operations[id], nil
}

func (s *projectTaskEdgeStore) OperationByIdempotency(deviceID string, kind edge.OperationKind, key string) (edge.Operation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range s.operations {
		if op.DeviceID == deviceID && op.Kind == kind && op.Request.IdempotencyKey == key {
			return op, true, nil
		}
	}
	return edge.Operation{}, false, nil
}

func (*projectTaskEdgeStore) ActiveOperations(string, int) ([]edge.Operation, error) { return nil, nil }
func (s *projectTaskEdgeStore) OperationLifecycleStatus(id string) (edge.Operation, error) {
	return s.OperationStatus(id)
}
func (s *projectTaskEdgeStore) RequestOperationCancel(id string) (edge.Operation, error) {
	return s.OperationStatus(id)
}
func (*projectTaskEdgeStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}

func TestProjectTaskStartsDistinctFencedCodexWorkersAndReconcilesCompletion(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)

	entry, ok := server.table["project_task_start"]
	if !ok {
		t.Fatal("project_task_start is not registered")
	}
	output, err := entry.handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Inspect package alpha and commit its focused fix.","Inspect package beta and commit its focused fix."],"timeout_seconds":600,"idempotency_key":"parallel-task-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		TaskID  string `json:"task_id"`
		State   string `json:"state"`
		Workers []struct {
			Ordinal     int    `json:"ordinal"`
			WorktreeID  string `json:"worktree_id"`
			WorkspaceID string `json:"workspace_id"`
			RuntimeID   string `json:"runtime_id"`
			Branch      string `json:"branch"`
		} `json:"workers"`
	}
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatal(err)
	}
	if view.TaskID == "" || view.State != "running" || len(view.Workers) != 2 || view.Workers[0].WorktreeID == view.Workers[1].WorktreeID || view.Workers[0].WorkspaceID == view.Workers[1].WorkspaceID || view.Workers[0].RuntimeID == view.Workers[1].RuntimeID {
		t.Fatalf("view=%+v output=%s", view, output)
	}
	for _, worker := range view.Workers {
		if !strings.HasPrefix(worker.Branch, "codex/worktree-") {
			t.Fatalf("branch=%q", worker.Branch)
		}
		runtime, err := turns.Runtime(context.Background(), worker.RuntimeID)
		if err != nil || runtime.State != modelturn.RuntimeStateAwaitingEdge || runtime.WorkspaceID != worker.WorkspaceID {
			t.Fatalf("runtime=%+v err=%v", runtime, err)
		}
		if err := turns.CompleteRuntime(context.Background(), worker.RuntimeID); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, found, err := queue.Task(view.TaskID)
	if err != nil || !found || completed.State != workqueue.TaskCompleted {
		t.Fatalf("completed=%+v found=%v err=%v", completed, found, err)
	}
}

func TestProjectTaskCancellationCancelsEveryRuntimeIdempotently(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	server.WithEdgeStore(newProjectTaskEdgeStore()).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit focused alpha fix.","Commit focused beta fix."],"timeout_seconds":600,"idempotency_key":"parallel-cancel-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatal(err)
	}
	cancelled, err := server.table["project_task_cancel"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := server.table["project_task_cancel"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || cancelled != repeated || !strings.Contains(cancelled, `"state":"cancelled"`) {
		t.Fatalf("cancelled=%s repeated=%s err=%v", cancelled, repeated, err)
	}
	for _, worker := range started.Workers {
		runtime, err := turns.Runtime(context.Background(), worker.RuntimeID)
		if err != nil || runtime.State != modelturn.RuntimeStateCancelled {
			t.Fatalf("runtime=%+v err=%v", runtime, err)
		}
	}
}

func TestProjectTaskCancellationBeforeRuntimeDoesNotStartWorker(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	goal, err := turns.StageRuntimeGoal(context.Background(), []byte("bounded task"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := queue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: "parallel-prestart-cancel-0001", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: goal.ContentDigest,
		WorkerGoalHashes: []string{goal.ContentDigest}, WorkerGoalRefs: []string{goal.BodyRef},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := queue.LeaseTaskWorker(task.ID, 0, server.projectTaskHolder(), projectTaskLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileProjectTask(context.Background(), task.ID, false); err != nil {
		t.Fatal(err)
	}
	terminal, found, err := queue.Task(task.ID)
	if err != nil || !found || terminal.State != workqueue.TaskCancelled || terminal.Workers[0].RuntimeID != "" || terminal.Workers[0].WorktreeID != "" {
		t.Fatalf("worker=%+v found=%v err=%v leased=%+v", terminal.Workers, found, err, worker)
	}
	edges.mu.Lock()
	createdWorktrees := len(edges.worktrees)
	edges.mu.Unlock()
	if createdWorktrees != 0 {
		t.Fatalf("cancelled worker created %d worktrees", createdWorktrees)
	}
}

func TestProjectTaskStatusCleanupAndCoordinatorLifecycle(t *testing.T) {
	var nilServer *Server
	nilServer.StartProjectTaskCoordinator(context.Background())
	nilServer.StopProjectTaskCoordinator()

	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)

	server.StartProjectTaskCoordinator(context.Background())
	server.StartProjectTaskCoordinator(context.Background())
	server.StopProjectTaskCoordinator()
	server.StopProjectTaskCoordinator()

	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-cleanup-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatal(err)
	}
	if len(started.Workers) != 1 {
		t.Fatalf("started=%+v", started)
	}
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `","idempotency_key":"parallel-cleanup-early-0001"}`)); err == nil {
		t.Fatal("nonterminal task cleanup was accepted")
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) || !strings.Contains(status, `"lifecycle_state":"completed"`) ||
		!strings.Contains(status, `"runtime_state":"completed"`) || !strings.Contains(status, `"acceptance_state":"pending"`) ||
		!strings.Contains(status, `"base_commit":"0123456789abcdef0123456789abcdef01234567"`) ||
		!strings.Contains(status, `"head_commit":"1123456789abcdef0123456789abcdef01234567"`) ||
		!strings.Contains(status, `"clean":true`) || !strings.Contains(status, `"commits_ahead_base":1`) ||
		!strings.Contains(status, `"changed_path_count":1`) || strings.Contains(status, `"state":"succeeded"`) {
		t.Fatalf("status=%s err=%v", status, err)
	}
	cleaned, err := server.table["project_task_cleanup"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `","idempotency_key":"parallel-cleanup-finish-0001"}`))
	if err != nil || !strings.Contains(cleaned, `"cleaned":true`) {
		t.Fatalf("cleaned=%s err=%v", cleaned, err)
	}

	queued, _, err := queue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: "parallel-cleanup-no-worktree", Project: "project", Target: "parrot",
		BaseCommit: strings.Repeat("a", 40), GoalHash: "sha256:" + strings.Repeat("b", 64),
		WorkerGoalHashes: []string{"sha256:" + strings.Repeat("1", 64)}, WorkerGoalRefs: []string{"mb_11111111111111111111111111111111"},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.CancelTask(queued.ID); err != nil {
		t.Fatal(err)
	}
	cleaned, err = server.table["project_task_cleanup"].handler(json.RawMessage(`{"task_id":"` + queued.ID + `","idempotency_key":"parallel-cleanup-empty-0001"}`))
	if err != nil || !strings.Contains(cleaned, `"cleaned":true`) {
		t.Fatalf("empty cleanup=%s err=%v", cleaned, err)
	}
}

func TestProjectTaskStatusRequiresLiveGitEvidenceForCompletedRuntime(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-evidence-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatal(err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	server.WithEdgeStore(&inactiveProjectTaskEdgeStore{edges})
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"reconciliation_required"`) ||
		!strings.Contains(status, `"runtime_state":"completed"`) || !strings.Contains(status, `"acceptance_state":"reconciliation_required"`) ||
		strings.Contains(status, `"git_evidence_known":true`) || strings.Contains(status, `"state":"succeeded"`) {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestProjectTaskSemanticStatePrecedenceIsDeterministic(t *testing.T) {
	cases := []struct {
		name    string
		workers []projectTaskWorkerView
		want    string
	}{
		{name: "all pending", workers: []projectTaskWorkerView{{State: "acceptance_pending"}, {State: "acceptance_pending"}}, want: "acceptance_pending"},
		{name: "failure dominates reconciliation", workers: []projectTaskWorkerView{{State: "reconciliation_required"}, {State: "failed"}}, want: "failed"},
		{name: "reconciliation dominates running", workers: []projectTaskWorkerView{{State: "running"}, {State: "reconciliation_required"}}, want: "reconciliation_required"},
		{name: "running dominates cancellation", workers: []projectTaskWorkerView{{State: "cancelled"}, {State: "running"}}, want: "running"},
		{name: "terminal cancellation mix", workers: []projectTaskWorkerView{{State: "acceptance_pending"}, {State: "cancelled"}}, want: "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectTaskViewSemanticState(tc.workers); got != tc.want {
				t.Fatalf("state=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestProjectTaskSemanticStatesRemainHonestWithoutReconciliationStores(t *testing.T) {
	taskCases := []struct {
		state workqueue.TaskState
		want  string
	}{
		{state: workqueue.TaskCompleted, want: "acceptance_pending"},
		{state: workqueue.TaskFailed, want: "failed"},
		{state: workqueue.TaskCancelled, want: "cancelled"},
		{state: workqueue.TaskRunning, want: "running"},
	}
	for _, tc := range taskCases {
		if got := projectTaskSemanticState(tc.state); got != tc.want {
			t.Fatalf("task state %s mapped to %s, want %s", tc.state, got, tc.want)
		}
	}

	workerCases := []struct {
		state       workqueue.State
		wantState   string
		wantRuntime string
		wantAccept  string
	}{
		{state: workqueue.StateSucceeded, wantState: "acceptance_pending", wantRuntime: string(modelturn.RuntimeStateCompleted), wantAccept: "pending"},
		{state: workqueue.StateFailed, wantState: "failed", wantRuntime: string(modelturn.RuntimeStateFailed), wantAccept: "failed"},
		{state: workqueue.StateCancelled, wantState: "cancelled", wantRuntime: string(modelturn.RuntimeStateCancelled), wantAccept: "cancelled"},
		{state: workqueue.StateLeased, wantState: "running", wantRuntime: "", wantAccept: "not_ready"},
	}
	for _, tc := range workerCases {
		state, runtimeState, acceptance := projectTaskWorkerSemanticState(workqueue.TaskWorker{State: tc.state})
		if state != tc.wantState || runtimeState != tc.wantRuntime || acceptance != tc.wantAccept {
			t.Fatalf("worker state %s mapped to (%s,%s,%s), want (%s,%s,%s)", tc.state, state, runtimeState, acceptance, tc.wantState, tc.wantRuntime, tc.wantAccept)
		}
	}

	task := workqueue.TaskGroup{State: workqueue.TaskCompleted, Workers: []workqueue.TaskWorker{{State: workqueue.StateSucceeded}}}
	view := (&Server{}).projectTaskStatusView(context.Background(), task)
	if view.State != "reconciliation_required" || len(view.Workers) != 1 || view.Workers[0].RuntimeState != "unknown" || view.Workers[0].AcceptanceState != "reconciliation_required" {
		t.Fatalf("view without reconciliation stores=%+v", view)
	}
}

func TestProjectTaskToolsFailClosedWithoutRequiredStores(t *testing.T) {
	server, _ := modelTurnServer(t)
	for name, body := range map[string]string{
		"project_task_start":   `{"alias":"project","target":"parrot","goals":["goal"],"timeout_seconds":60,"idempotency_key":"parallel-missing-0001"}`,
		"project_task_status":  `{"task_id":"tg_11111111111111111111111111111111"}`,
		"project_task_cancel":  `{"task_id":"tg_11111111111111111111111111111111"}`,
		"project_task_cleanup": `{"task_id":"tg_11111111111111111111111111111111","idempotency_key":"parallel-missing-cleanup"}`,
	} {
		if _, err := server.table[name].handler(json.RawMessage(body)); err == nil {
			t.Fatalf("%s accepted missing durable work queue", name)
		}
	}
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	server.WithWorkQueue(queue)
	turnStore := server.modelTurns
	server.modelTurns = nil
	if _, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["goal"],"timeout_seconds":60,"idempotency_key":"parallel-missing-model"}`)); !errors.Is(err, errModelTurnStoreUnavailable) {
		t.Fatalf("missing model-turn store error=%v", err)
	}
	server.modelTurns = turnStore
	if _, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["goal"],"timeout_seconds":60,"idempotency_key":"parallel-missing-edge-01"}`)); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("missing edge store error=%v", err)
	}
	if _, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"tg_11111111111111111111111111111111"}`)); err == nil {
		t.Fatal("unknown task status was accepted")
	}
	if _, err := server.table["project_task_cancel"].handler(json.RawMessage(`{"task_id":"tg_11111111111111111111111111111111"}`)); err == nil {
		t.Fatal("unknown task cancellation was accepted")
	}
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(`{"task_id":"tg_11111111111111111111111111111111","idempotency_key":"parallel-missing-cleanup"}`)); !errors.Is(err, errEdgeStoreUnavailable) {
		t.Fatalf("cleanup error=%v", err)
	}
	server.WithEdgeStore(&inactiveProjectTaskEdgeStore{newProjectTaskEdgeStore()})
	if _, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["goal"],"timeout_seconds":60,"idempotency_key":"parallel-inactive-edge-01"}`)); err == nil {
		t.Fatal("inactive edge accepted a task")
	}
	server.WithEdgeStore(newProjectTaskEdgeStore())
	for _, name := range []string{"project_task_start", "project_task_status", "project_task_cancel", "project_task_cleanup"} {
		if _, err := server.table[name].handler(json.RawMessage(`{"broken"`)); err == nil {
			t.Fatalf("%s accepted malformed input", name)
		}
	}
}

func TestProjectTaskReclaimsWorktreeBeforeResumingRuntime(t *testing.T) {
	server, _ := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-reclaim-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatal(err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found {
		t.Fatal(err)
	}
	worker := task.Workers[0]
	worker.Attempt = 2
	worker.LeaseID = "wl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	worker.Fence++
	if err := server.claimProjectTaskWorktree(context.Background(), task, worker, edge.Device{ID: testEdgeDeviceID}, false); err != nil {
		t.Fatal(err)
	}
	edges.mu.Lock()
	claimed := edges.worktrees[worker.WorktreeID]
	edges.mu.Unlock()
	if claimed.WorkLeaseID != worker.LeaseID || claimed.WorkFence != worker.Fence {
		t.Fatalf("claim=%+v worker=%+v", claimed, worker)
	}
	if err := server.claimProjectTaskWorktree(context.Background(), task, worker, edge.Device{ID: testEdgeDeviceID}, false); err != nil {
		t.Fatal(err)
	}
}

func TestProjectTaskRecoversOperationCreatedBeforeBinding(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	goal, err := turns.StageRuntimeGoal(context.Background(), []byte("bounded task"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := queue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: "parallel-operation-gap-0001", Project: "project", Target: "parrot",
		BaseCommit: "0123456789abcdef0123456789abcdef01234567", GoalHash: goal.ContentDigest,
		WorkerGoalHashes: []string{goal.ContentDigest}, WorkerGoalRefs: []string{goal.BodyRef},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := queue.LeaseTaskWorker(task.ID, 0, server.projectTaskHolder(), projectTaskLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	key := task.ID + ":worker:0"
	created, wasCreated, err := edges.CreateOperation(testEdgeDeviceID, edge.OperationProjectWorktreeCreate, edge.OperationRequest{
		Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", IdempotencyKey: key,
		WorktreeBaseCommit: task.BaseCommit, WorktreeRole: "writer", WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence,
	})
	if err != nil || !wasCreated {
		t.Fatalf("operation=%+v created=%v err=%v", created, wasCreated, err)
	}
	if err := server.reconcileProjectTask(context.Background(), task.ID, true); err != nil {
		t.Fatal(err)
	}
	recovered, found, err := queue.Task(task.ID)
	if err != nil || !found || recovered.Workers[0].OperationID != created.ID || recovered.Workers[0].WorktreeID == "" || recovered.Workers[0].RuntimeID == "" {
		t.Fatalf("recovered=%+v found=%v err=%v", recovered, found, err)
	}
	edges.mu.Lock()
	createCount := 0
	for _, op := range edges.operations {
		if op.Kind == edge.OperationProjectWorktreeCreate {
			createCount++
		}
	}
	edges.mu.Unlock()
	if createCount != 1 {
		t.Fatalf("worktree create operations=%d", createCount)
	}
}

func TestProjectTaskCancellationRecoversUnboundOperationWithoutStartingRuntime(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	goal, err := turns.StageRuntimeGoal(context.Background(), []byte("bounded task"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := queue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: "parallel-gap-cancel-0001", Project: "project", Target: "parrot",
		BaseCommit: "0123456789abcdef0123456789abcdef01234567", GoalHash: goal.ContentDigest,
		WorkerGoalHashes: []string{goal.ContentDigest}, WorkerGoalRefs: []string{goal.BodyRef},
		Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := queue.LeaseTaskWorker(task.ID, 0, server.projectTaskHolder(), projectTaskLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := edges.CreateOperation(testEdgeDeviceID, edge.OperationProjectWorktreeCreate, edge.OperationRequest{
		Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", IdempotencyKey: projectTaskWorktreeOperationKey(task.ID, 0),
		WorktreeBaseCommit: task.BaseCommit, WorktreeRole: "writer", WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.CancelTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileProjectTask(context.Background(), task.ID, true); err != nil {
		t.Fatal(err)
	}
	recovered, found, err := queue.Task(task.ID)
	if err != nil || !found || recovered.State != workqueue.TaskCancelled || recovered.Workers[0].OperationID != created.ID || recovered.Workers[0].WorktreeID == "" || recovered.Workers[0].RuntimeID != "" {
		t.Fatalf("recovered=%+v found=%v err=%v", recovered, found, err)
	}
}

func TestProjectTaskRejectsInvalidLaterGoalAndCoversTerminalClassifiers(t *testing.T) {
	server, _ := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	server.WithEdgeStore(newProjectTaskEdgeStore()).WithWorkQueue(queue)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["valid first goal","   "],"timeout_seconds":600,"idempotency_key":"parallel-invalid-0001"}`)); err == nil {
		t.Fatal("blank second goal was accepted")
	}
	if projectTaskGoalSummary("short") != "" {
		t.Fatal("short digest produced a summary")
	}
	for _, state := range []modelturn.RuntimeState{modelturn.RuntimeStateFailed, modelturn.RuntimeStateCancelled, modelturn.RuntimeStateExpired} {
		_, _, terminal := projectTaskRuntimeOutcome(modelturn.Runtime{State: state})
		if !terminal {
			t.Fatalf("state %s was not terminal", state)
		}
	}
	if _, _, terminal := projectTaskRuntimeOutcome(modelturn.Runtime{State: modelturn.RuntimeStateAwaitingEdge}); terminal {
		t.Fatal("nonterminal runtime was classified as terminal")
	}
}
