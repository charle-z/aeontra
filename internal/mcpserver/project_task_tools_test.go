package mcpserver

import (
	"context"
	"database/sql"
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
	mu                     sync.Mutex
	next                   int
	snapshotRequests       int
	worktreeStatusRequests int
	testStatusRequests     int
	testProcessState       string
	testExitKnown          bool
	testExitCode           int
	testStale              bool
	testProfileUnavailable bool
	operations             map[string]edge.Operation
	workspaces             map[string]edge.WorkspaceBinding
	worktrees              map[string]edge.OperationResult
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
	if kind == edge.OperationProjectWorktreeStatus {
		s.worktreeStatusRequests++
	}
	if kind == edge.OperationProjectSnapshot {
		s.snapshotRequests++
	}
	if kind == edge.OperationProjectWorktreeTestStatus {
		s.testStatusRequests++
	}
	for _, existing := range s.operations {
		// Fresh process observations have no idempotency key in the real store.
		if kind != edge.OperationProjectWorktreeTestStatus && kind != edge.OperationProjectProcessStatus && existing.DeviceID == deviceID && existing.Kind == kind && reflect.DeepEqual(existing.Request, request) {
			return existing, false, nil
		}
	}
	s.next++
	id := fmt.Sprintf("eo_%032x", s.next)
	op := edge.Operation{ID: id, DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}
	s.operations[id] = op
	return op, true, nil
}

func TestProjectTaskExactReplayWorksOfflineWithoutRestaging(t *testing.T) {
	server := stampServer(t)
	quota := int64(1024)
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "model-turns"), QuotaBytes: quota})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-replay-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithModelTurnStore(turns).WithEdgeStore(edges).WithWorkQueue(queue)

	const request = `{"alias":"project","target":"parrot","goals":["Retain this exact task goal."],"timeout_seconds":600,"idempotency_key":"parallel-replay-offline-01"}`
	first, err := server.table["project_task_start"].handler(json.RawMessage(request))
	if err != nil {
		t.Fatal(err)
	}
	var original projectTaskView
	if err := json.Unmarshal([]byte(first), &original); err != nil {
		t.Fatal(err)
	}
	bodies, _, err := projectTaskGoalBodies([]string{"Retain this exact task goal."})
	if err != nil {
		t.Fatal(err)
	}
	filler := make([]byte, int(quota)-len(bodies[0]))
	if _, err := turns.StageRuntimeGoal(context.Background(), filler, modelturn.MaxTurnTTL); err != nil {
		t.Fatalf("filling the remaining bounded goal quota: %v", err)
	}

	edges.mu.Lock()
	snapshotCount := edges.snapshotRequests
	edges.mu.Unlock()
	server.edgeOperations = nil
	server.edgeDevices = nil
	server.edgeWorkspaces = nil
	replayed, err := server.table["project_task_start"].handler(json.RawMessage(request))
	if err != nil {
		t.Fatalf("exact replay with Edge unavailable and goal quota full: %v", err)
	}
	var replay projectTaskView
	if err := json.Unmarshal([]byte(replayed), &replay); err != nil || replay.TaskID != original.TaskID {
		t.Fatalf("replay=%+v original=%+v err=%v", replay, original, err)
	}
	edges.mu.Lock()
	gotSnapshots := edges.snapshotRequests
	edges.mu.Unlock()
	if gotSnapshots != snapshotCount {
		t.Fatalf("snapshot requests changed from %d to %d during replay", snapshotCount, gotSnapshots)
	}

	changedGoal := strings.Replace(request, "Retain this exact task goal.", "A different task goal.", 1)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(changedGoal)); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed goal replay error=%v", err)
	}
	changedTimeout := strings.Replace(request, `"timeout_seconds":600`, `"timeout_seconds":601`, 1)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(changedTimeout)); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed timeout replay error=%v", err)
	}
}

func TestProjectTaskStartPinsOperatorTestProfileAndRejectsConflictingReplay(t *testing.T) {
	server, _ := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-profile"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)

	const request = `{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-profile-0001","test_profile_id":"go-check"}`
	output, err := server.table["project_task_start"].handler(json.RawMessage(request))
	if err != nil {
		t.Fatal(err)
	}
	var view projectTaskView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatal(err)
	}
	task, found, err := queue.Task(view.TaskID)
	if err != nil || !found || task.TestAcceptanceContract == nil || task.TestAcceptanceContract.ProfileID != "go-check" || task.TestAcceptanceContract.ProfileDigest == "" {
		t.Fatalf("test profile contract not durably pinned: task=%+v found=%v err=%v", task, found, err)
	}
	server.edgeOperations = nil
	server.edgeDevices = nil
	server.edgeWorkspaces = nil
	if _, err := server.table["project_task_start"].handler(json.RawMessage(request)); err != nil {
		t.Fatalf("exact replay should be offline-safe: %v", err)
	}
	conflict := strings.Replace(request, `"test_profile_id":"go-check"`, `"test_profile_id":"other-check"`, 1)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(conflict)); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed profile replay error=%v", err)
	}
}

func TestProjectTaskStartCanRetryAfterOperatorRepairsTestProfile(t *testing.T) {
	server, _ := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-profile-retry"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	edges.testProfileUnavailable = true
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	request := json.RawMessage(`{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-profile-retry-0001","test_profile_id":"go-check"}`)
	if _, err := server.table["project_task_start"].handler(request); err == nil {
		t.Fatal("missing operator profile unexpectedly created a task")
	}
	edges.mu.Lock()
	edges.testProfileUnavailable = false
	edges.mu.Unlock()
	if _, err := server.table["project_task_start"].handler(request); err != nil {
		t.Fatalf("fixed operator profile remained blocked by a cached failed lookup: %v", err)
	}
	edges.mu.Lock()
	keys := make(map[string]struct{})
	for _, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeTestProfile {
			keys[operation.Request.IdempotencyKey] = struct{}{}
		}
	}
	edges.mu.Unlock()
	if len(keys) != 2 {
		t.Fatalf("profile retry reused an old failed Edge operation: distinct keys=%d", len(keys))
	}
}

func TestProjectTaskTestEvidenceRequiresTerminalZeroExitAndLiveRevalidation(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-evidence"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-evidence-0001","test_profile_id":"go-check"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil || len(started.Workers) != 1 {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(fmt.Sprintf(`{"task_id":%q,"ordinal":0}`, started.TaskID))
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q,"idempotency_key":"test-cleanup-early-0001"}`, started.TaskID))); err == nil || !strings.Contains(err.Error(), "test evidence receipt is required") {
		t.Fatalf("cleanup before test evidence error=%v", err)
	}
	startResponse, err := server.table["project_task_test_start"].handler(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(startResponse, `"state":"status_required"`) || strings.Contains(startResponse, `"process_state":`) {
		t.Fatalf("start claimed a current process state from replayable evidence: %s", startResponse)
	}
	running, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(running, `"test_evidence_state":"running"`) {
		t.Fatalf("running test status=%s err=%v", running, err)
	}
	edges.mu.Lock()
	edges.testProcessState, edges.testExitKnown, edges.testExitCode = "exited", true, 0
	edges.mu.Unlock()
	passed, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(passed, `"test_evidence_state":"verified"`) || !strings.Contains(passed, `"acceptance_state":"pending"`) {
		t.Fatalf("passing test status=%s err=%v", passed, err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found || task.Workers[0].TestAcceptanceReceipt == nil {
		t.Fatalf("receipt missing: task=%+v found=%v err=%v", task, found, err)
	}
	edges.mu.Lock()
	edges.testStale = true
	edges.mu.Unlock()
	stale, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(stale, `"test_evidence_state":"stale"`) {
		t.Fatalf("stale test status=%s err=%v", stale, err)
	}
	taskStatus, err := server.table["project_task_status"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, started.TaskID)))
	if err != nil || !strings.Contains(taskStatus, `"reconciliation_reason":"test_evidence_stale"`) {
		t.Fatalf("stale task status=%s err=%v", taskStatus, err)
	}
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q,"idempotency_key":"test-cleanup-stale-0001"}`, started.TaskID))); err == nil || !strings.Contains(err.Error(), "test evidence changed") {
		t.Fatalf("cleanup with stale test evidence error=%v", err)
	}
}

func TestProjectTaskTestNonzeroExitNeverRecordsPassingReceipt(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-nonzero"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-nonzero-0001","test_profile_id":"go-check"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(output), &started); err != nil || len(started.Workers) != 1 {
		t.Fatalf("started=%+v err=%v", started, err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(fmt.Sprintf(`{"task_id":%q,"ordinal":0}`, started.TaskID))
	if _, err := server.table["project_task_test_start"].handler(request); err != nil {
		t.Fatal(err)
	}
	edges.mu.Lock()
	edges.testProcessState, edges.testExitKnown, edges.testExitCode = "exited", true, 1
	edges.mu.Unlock()
	failed, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(failed, `"test_evidence_state":"failed"`) {
		t.Fatalf("failed test status=%s err=%v", failed, err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found || task.Workers[0].TestAcceptanceReceipt != nil {
		t.Fatalf("nonzero exit recorded receipt: task=%+v found=%v err=%v", task, found, err)
	}
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q,"idempotency_key":"test-cleanup-nonzero-0001"}`, started.TaskID))); err == nil || !strings.Contains(err.Error(), "test evidence receipt is required") {
		t.Fatalf("cleanup after nonzero test error=%v", err)
	}
}

func TestProjectTaskTestStopDoesNotAcceptWorker(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-stop"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-stop-0001","test_profile_id":"go-check"}`))
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
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(fmt.Sprintf(`{"task_id":%q,"ordinal":0}`, started.TaskID))
	if _, err := server.table["project_task_test_stop"].handler(request); err == nil {
		t.Fatal("stop without a captured test process unexpectedly succeeded")
	}
	if _, err := server.table["project_task_test_start"].handler(request); err != nil {
		t.Fatal(err)
	}
	stopped, err := server.table["project_task_test_stop"].handler(request)
	if err != nil || !strings.Contains(stopped, `"state":"stopping"`) || !strings.Contains(stopped, `"acceptance_state":"pending"`) {
		t.Fatalf("stop=%s err=%v", stopped, err)
	}
	edges.mu.Lock()
	edges.testProcessState = "stopped"
	edges.mu.Unlock()
	status, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(status, `"test_evidence_state":"failed"`) {
		t.Fatalf("status after stop=%s err=%v", status, err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found || task.Workers[0].TestAcceptanceReceipt != nil {
		t.Fatalf("stop recorded passing receipt: task=%+v found=%v err=%v", task, found, err)
	}
}

func TestProjectTaskTestReceiptSurvivesCleanup(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-test-cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	output, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Make one focused change."],"timeout_seconds":600,"idempotency_key":"task-test-cleanup-0001","test_profile_id":"go-check"}`))
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
	if err := server.reconcileProjectTasksOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(fmt.Sprintf(`{"task_id":%q,"ordinal":0}`, started.TaskID))
	if _, err := server.table["project_task_test_start"].handler(request); err != nil {
		t.Fatal(err)
	}
	edges.mu.Lock()
	edges.testProcessState, edges.testExitKnown = "exited", true
	edges.mu.Unlock()
	if _, err := server.table["project_task_test_status"].handler(request); err != nil {
		t.Fatal(err)
	}
	cleaned, err := server.table["project_task_cleanup"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q,"idempotency_key":"task-test-cleanup-call-0001"}`, started.TaskID)))
	if err != nil || !strings.Contains(cleaned, `"cleaned":true`) || !strings.Contains(cleaned, `"test_evidence_state":"verified"`) {
		t.Fatalf("cleanup=%s err=%v", cleaned, err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, started.TaskID)))
	if err != nil || !strings.Contains(status, `"test_evidence_state":"verified"`) || !strings.Contains(status, `"acceptance_state":"pending"`) {
		t.Fatalf("status after cleanup=%s err=%v", status, err)
	}
	testStatus, err := server.table["project_task_test_status"].handler(request)
	if err != nil || !strings.Contains(testStatus, `"test_evidence_state":"verified"`) {
		t.Fatalf("test status after cleanup=%s err=%v", testStatus, err)
	}
}

func TestProjectTaskTestCapturedStopRemainsBoundAfterLeaseRotation(t *testing.T) {
	task := workqueue.TaskGroup{ID: "tg_11111111111111111111111111111111", Project: "project", Target: "parrot", BaseCommit: "0123456789abcdef0123456789abcdef01234567", TestAcceptanceContract: &workqueue.TaskTestAcceptanceContract{Version: 1, ProfileID: "go-check", ProfileDigest: "sha256:" + strings.Repeat("e", 64)}}
	worker := workqueue.TaskWorker{Ordinal: 0, JobID: "j_11111111111111111111111111111111", WorktreeID: "wt_11111111111111111111111111111111", WorkspaceID: "ws_11111111111111111111111111111111", LeaseID: "le_new", Fence: 2}
	start := edge.Operation{DeviceID: testEdgeDeviceID, Kind: edge.OperationProjectWorktreeTestStart, State: edge.OperationSucceeded}
	start.Request = edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID, WorkJobID: worker.JobID, WorkLeaseID: "le_old", WorkFence: 1, TestProfileID: task.TestAcceptanceContract.ProfileID, TestProfileDigest: task.TestAcceptanceContract.ProfileDigest, IdempotencyKey: projectTaskTestOperationKey(task.ID, 0, "start")}
	start.Result = edge.OperationResult{WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID, WorkJobID: worker.JobID, WorkLeaseID: "le_old", WorkFence: 1, WorktreeBaseCommit: task.BaseCommit, WorktreeHeadCommit: "1123456789abcdef0123456789abcdef01234567", WorktreeBranch: "codex/worktree-11111111111111111111111111111111", ContentDigest: "sha256:" + strings.Repeat("b", 64), TestProfileID: task.TestAcceptanceContract.ProfileID, TestProfileDigest: task.TestAcceptanceContract.ProfileDigest, BackgroundProcessID: "pr_11111111111111111111111111111111"}
	if !validProjectTaskTestCapturedStart(task, worker, testEdgeDeviceID, start) {
		t.Fatal("captured process lost stop authority after lease rotation")
	}
	if validProjectTaskTestStart(task, worker, testEdgeDeviceID, start) {
		t.Fatal("old lease incorrectly remains valid for passing test evidence")
	}
}

func TestProjectTaskReplayFailsClosedBeforeEdgeForMissingOrMismatchedGoal(t *testing.T) {
	for _, test := range []struct {
		name       string
		missingRef bool
	}{
		{name: "missing", missingRef: true},
		{name: "mismatched"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, turns := modelTurnServer(t)
			queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-goal-fail-test"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = queue.Close() })
			edges := newProjectTaskEdgeStore()
			server.WithEdgeStore(edges).WithWorkQueue(queue)
			const key = "parallel-goal-integrity-01"
			const request = `{"alias":"project","target":"parrot","goals":["Inspect the exact goal body."],"timeout_seconds":600,"idempotency_key":"parallel-goal-integrity-01"}`
			_, hashes, err := projectTaskGoalBodies([]string{"Inspect the exact goal body."})
			if err != nil {
				t.Fatal(err)
			}
			goalRef := "mb_11111111111111111111111111111111"
			if !test.missingRef {
				other, err := turns.StageRuntimeGoal(context.Background(), []byte("different immutable body"), modelturn.MaxTurnTTL)
				if err != nil {
					t.Fatal(err)
				}
				goalRef = other.BodyRef
			}
			_, _, err = queue.CreateTask(workqueue.TaskSpec{
				IdempotencyKey: key, Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40),
				GoalHash: projectTaskGroupHash(hashes), WorkerGoalHashes: hashes, WorkerGoalRefs: []string{goalRef},
				Pool: "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 1, ExecutionTimeoutSeconds: 600,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := server.table["project_task_start"].handler(json.RawMessage(request)); !errors.Is(err, modelturn.ErrRequestRefConflict) {
				t.Fatalf("invalid task goal replay error=%v", err)
			}
			edges.mu.Lock()
			snapshotRequests, operations := edges.snapshotRequests, len(edges.operations)
			edges.mu.Unlock()
			if snapshotRequests != 0 || operations != 0 {
				t.Fatalf("invalid goal caused Edge effects: snapshots=%d operations=%d", snapshotRequests, operations)
			}
		})
	}
}

func TestProjectTaskStatusMarksOnlyWorkerWithUnavailableGoal(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-goal-status-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	bodies, hashes, err := projectTaskGoalBodies([]string{"Valid worker goal.", "Unavailable worker goal."})
	if err != nil {
		t.Fatal(err)
	}
	validGoal, err := turns.StageRuntimeGoal(context.Background(), bodies[0], modelturn.MaxTurnTTL)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := queue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: "parallel-goal-status-0001", Project: "project", Target: "parrot", BaseCommit: strings.Repeat("a", 40),
		GoalHash: projectTaskGroupHash(hashes), WorkerGoalHashes: hashes,
		WorkerGoalRefs: []string{validGoal.BodyRef, "mb_11111111111111111111111111111111"},
		Pool:           "edge.parrot.runtime", Profile: "codex.worker", WorkerCount: 2, ExecutionTimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	leased, err := queue.LeaseTaskWorker(task.ID, 1, server.projectTaskHolder(), projectTaskLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	const worktreeID = "wt_22222222222222222222222222222222"
	const workspaceID = "ws_22222222222222222222222222222222"
	const runtimeID = "mr_22222222222222222222222222222222"
	if _, err := queue.BindTaskWorker(workqueue.TaskWorkerBinding{
		TaskID: task.ID, Ordinal: 1, JobID: leased.JobID, LeaseID: leased.LeaseID, Fence: leased.Fence,
		WorktreeID: worktreeID, WorkspaceID: workspaceID, RuntimeID: runtimeID,
	}); err != nil {
		t.Fatal(err)
	}
	output, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + task.ID + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	var view projectTaskView
	if err := json.Unmarshal([]byte(output), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Workers) != 2 || view.Workers[0].Attention == "task_goal_unavailable" || view.Workers[0].State == "reconciliation_required" ||
		view.Workers[1].State != "reconciliation_required" || view.Workers[1].Attention != "task_goal_unavailable" || view.Workers[1].ReconciliationReason != "task_goal_unavailable" ||
		view.Workers[1].WorktreeID != worktreeID || view.Workers[1].WorkspaceID != workspaceID || view.Workers[1].RuntimeID != runtimeID {
		t.Fatalf("task status lost worker identity or marked the wrong worker: %+v", view)
	}
	edges.mu.Lock()
	operations := len(edges.operations)
	edges.mu.Unlock()
	if operations != 0 {
		t.Fatalf("missing goal status caused %d Edge operations", operations)
	}
}

func (s *projectTaskEdgeStore) WaitOperation(_ context.Context, id string, _ time.Duration) (edge.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.operations[id]
	op.State = edge.OperationSucceeded
	if op.Kind == edge.OperationProjectWorktreeTestProfile && s.testProfileUnavailable {
		op.State = edge.OperationFailed
		s.operations[id] = op
		return op, nil
	}
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
	case edge.OperationKind("project_worktree_test_profile"):
		result.TestProfileID = op.Request.TestProfileID
		result.TestProfileDigest = "sha256:" + strings.Repeat("e", 64)
		result.TestTimeoutSeconds = 600
	case edge.OperationProjectWorktreeTestStart, edge.OperationProjectWorktreeTestStatus, edge.OperationProjectWorktreeTestStop:
		result = s.worktrees[op.Request.WorktreeID]
		if result.WorktreeHeadCommit == "" {
			result.WorktreeHeadCommit = "1123456789abcdef0123456789abcdef01234567"
		}
		result.TestProfileID, result.TestProfileDigest = op.Request.TestProfileID, op.Request.TestProfileDigest
		result.ContentDigest = "sha256:" + strings.Repeat("b", 64)
		result.BackgroundProcessID = "pr_11111111111111111111111111111111"
		result.BackgroundProcessState = "running"
		if op.Kind == edge.OperationProjectWorktreeTestStatus {
			if s.testProcessState != "" {
				result.BackgroundProcessState = s.testProcessState
			}
			result.BackgroundExitKnown, result.BackgroundExitCode, result.TestStale = s.testExitKnown, s.testExitCode, s.testStale
		}
		if op.Kind == edge.OperationProjectWorktreeTestStop {
			result.BackgroundProcessState = "stopping"
		}
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
		if !result.WorktreeEvidenceKnown {
			result.WorktreeHeadCommit = "1123456789abcdef0123456789abcdef01234567"
			result.WorktreeClean = true
			result.WorktreeCommitsAheadBase = 1
			result.WorktreeChangedPathCount = 1
		}
		result.WorktreeEvidenceKnown = true
		s.worktrees[result.WorktreeID] = result
	case edge.OperationProjectWorktreeCleanup:
		result = s.worktrees[op.Request.WorktreeID]
		result.WorktreeState = "removed"
		result.WorktreeUpdatedAt = time.Unix(3, 0).UTC().Format(time.RFC3339Nano)
		s.worktrees[result.WorktreeID] = result
		for operationID, status := range s.operations {
			if status.Kind == edge.OperationProjectWorktreeStatus && status.Request.WorktreeID == result.WorktreeID {
				status.Result.WorktreeState = "removed"
				s.operations[operationID] = status
			}
		}
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
	if err := turns.SetRuntimeState(context.Background(), started.Workers[0].RuntimeID, modelturn.RuntimeStateAwaitingModel, modelturn.RuntimeStateAwaitingEdge); err != nil {
		t.Fatal(err)
	}
	active, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(active, `"continuation":{"state":"needs_model","next_tool":"model_turn_next"}`) ||
		!strings.Contains(active, `"attention":"needs_model"`) ||
		!strings.Contains(active, `"attention_order":[0]`) ||
		!strings.Contains(active, `"handoff":{"version":1,"revision":"sha256:`) {
		t.Fatalf("active continuation=%s err=%v", active, err)
	}
	if err := turns.SetRuntimeState(context.Background(), started.Workers[0].RuntimeID, modelturn.RuntimeStateDisconnected, modelturn.RuntimeStateAwaitingModel); err != nil {
		t.Fatal(err)
	}
	disconnected, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(disconnected, `"reconciliation_reason":"runtime_disconnected"`) || !strings.Contains(disconnected, `"attention":"reconcile"`) || strings.Contains(disconnected, `"state":"accepted"`) {
		t.Fatalf("disconnected continuation=%s err=%v", disconnected, err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	listed, listErr := server.table["project_task_list"].handler(json.RawMessage(`{"alias":"project","target":"parrot"}`))
	if listErr != nil || !strings.Contains(listed, `"task_id":"`+started.TaskID+`"`) ||
		!strings.Contains(listed, `"lifecycle_state":"completed"`) || !strings.Contains(listed, `"next_tool":"project_task_status"`) ||
		strings.Contains(listed, "Commit one focused change") || strings.Contains(listed, "runtime_id") {
		t.Fatalf("recovery list=%s err=%v", listed, listErr)
	}
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) || !strings.Contains(status, `"lifecycle_state":"completed"`) ||
		!strings.Contains(status, `"runtime_state":"completed"`) || !strings.Contains(status, `"acceptance_state":"pending"`) ||
		!strings.Contains(status, `"base_commit":"0123456789abcdef0123456789abcdef01234567"`) ||
		!strings.Contains(status, `"head_commit":"1123456789abcdef0123456789abcdef01234567"`) ||
		!strings.Contains(status, `"clean":true`) || !strings.Contains(status, `"commits_ahead_base":1`) ||
		!strings.Contains(status, `"changed_path_count":1`) || !strings.Contains(status, `"continuation":{"state":"review_required"}`) ||
		!strings.Contains(status, `"attention":"review_required"`) || strings.Contains(status, `"state":"succeeded"`) {
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

func TestProjectTaskContinuationKeepsModelChoiceAndFailuresVisible(t *testing.T) {
	tests := []struct {
		name       string
		view       projectTaskView
		state      string
		nextTool   string
		attention0 string
	}{
		{name: "empty", view: projectTaskView{}, state: "wait"},
		{name: "cancelled", view: projectTaskView{State: "cancelled"}, state: "none"},
		{name: "awaiting model", view: projectTaskView{Workers: []projectTaskWorkerView{{State: "running", RuntimeState: string(modelturn.RuntimeStateAwaitingModel)}}}, state: "needs_model", nextTool: "model_turn_next", attention0: "needs_model"},
		{name: "completed requires review", view: projectTaskView{Workers: []projectTaskWorkerView{{State: "acceptance_pending"}}}, state: "review_required", attention0: "review_required"},
		{name: "failure before pending turn", view: projectTaskView{Workers: []projectTaskWorkerView{{State: "running", RuntimeState: string(modelturn.RuntimeStateAwaitingModel)}, {State: "failed"}}}, state: "inspect_failure", attention0: "needs_model"},
		{name: "reconcile before failure", view: projectTaskView{Workers: []projectTaskWorkerView{{State: "failed"}, {State: "reconciliation_required"}}}, state: "reconcile", attention0: "inspect_failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setProjectTaskContinuation(&test.view)
			if test.view.Continuation == nil || test.view.Continuation.State != test.state || test.view.Continuation.NextTool != test.nextTool {
				t.Fatalf("continuation=%+v, want state=%q next_tool=%q", test.view.Continuation, test.state, test.nextTool)
			}
			if test.attention0 != "" && test.view.Workers[0].Attention != test.attention0 {
				t.Fatalf("worker attention=%q, want %q", test.view.Workers[0].Attention, test.attention0)
			}
		})
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
		!strings.Contains(status, `"reconciliation_reason":"edge_unavailable"`) ||
		!strings.Contains(status, `"last_runtime_phase":"terminal"`) || !strings.Contains(status, `"last_runtime_phase_at":`) ||
		strings.Contains(status, `"git_evidence_known":true`) || strings.Contains(status, `"state":"succeeded"`) {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestProjectTaskGitAcceptanceContractAcceptsOnlyMatchingLiveEvidence(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-acceptance-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)

	request := `{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-acceptance-0001","git_evidence_contract":{"version":1,"minimum_commits_ahead_per_worker":1,"minimum_changed_paths_per_worker":1}}`
	startedOutput, err := server.table["project_task_start"].handler(json.RawMessage(request))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(startedOutput), &started); err != nil {
		t.Fatal(err)
	}
	if started.TaskID == "" || !strings.Contains(startedOutput, `"git_evidence_contract":{"version":1,"minimum_commits_ahead_per_worker":1,"minimum_changed_paths_per_worker":1}`) {
		t.Fatalf("start omitted durable Git evidence contract: %s", startedOutput)
	}
	legacyField := strings.Replace(request, `"git_evidence_contract"`, `"acceptance_contract"`, 1)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(legacyField)); err == nil {
		t.Fatal("unshipped acceptance_contract alias was accepted")
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) || !strings.Contains(status, `"lifecycle_state":"completed"`) ||
		!strings.Contains(status, `"runtime_state":"completed"`) || !strings.Contains(status, `"acceptance_state":"pending"`) ||
		!strings.Contains(status, `"git_evidence_state":"verified"`) || !strings.Contains(status, `"git_evidence_recorded_at":`) ||
		strings.Contains(status, `"reconciliation_reason":`) ||
		!strings.Contains(status, `"git_evidence_known":true`) || !strings.Contains(status, `"clean":true`) ||
		!strings.Contains(status, `"commits_ahead_base":1`) || !strings.Contains(status, `"changed_path_count":1`) {
		t.Fatalf("status=%s err=%v", status, err)
	}
	edges.mu.Lock()
	for id, worktree := range edges.worktrees {
		worktree.WorktreeBranch = "codex/other"
		edges.worktrees[id] = worktree
	}
	for id, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeStatus {
			operation.Result.WorktreeBranch = "codex/other"
			edges.operations[id] = operation
		}
	}
	edges.mu.Unlock()
	mismatched, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(mismatched, `"reconciliation_reason":"worktree_evidence_mismatch"`) || strings.Contains(mismatched, `"state":"accepted"`) {
		t.Fatalf("mismatched worktree evidence status=%s err=%v", mismatched, err)
	}
	edges.mu.Lock()
	for id, worktree := range edges.worktrees {
		worktree.WorktreeBranch = started.Workers[0].Branch
		edges.worktrees[id] = worktree
	}
	for id, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeStatus {
			operation.Result.WorktreeBranch = started.Workers[0].Branch
			edges.operations[id] = operation
		}
	}
	edges.mu.Unlock()
	edges.mu.Lock()
	for id, worktree := range edges.worktrees {
		worktree.WorktreeClean = false
		edges.worktrees[id] = worktree
	}
	for id, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeStatus {
			operation.Result.WorktreeClean = false
			edges.operations[id] = operation
		}
	}
	edges.mu.Unlock()
	dirty, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(dirty, `"reconciliation_reason":"git_evidence_stale"`) || !strings.Contains(dirty, `"git_evidence_state":"stale"`) {
		t.Fatalf("dirty accepted worktree status=%s err=%v", dirty, err)
	}
	edges.mu.Lock()
	for id, worktree := range edges.worktrees {
		worktree.WorktreeClean = true
		edges.worktrees[id] = worktree
	}
	for id, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeStatus {
			operation.Result.WorktreeClean = true
			edges.operations[id] = operation
		}
	}
	edges.mu.Unlock()

	conflicting := strings.Replace(request, `"minimum_changed_paths_per_worker":1`, `"minimum_changed_paths_per_worker":2`, 1)
	if _, err := server.table["project_task_start"].handler(json.RawMessage(conflicting)); err == nil || !strings.Contains(err.Error(), "idempotency key conflicts") {
		t.Fatalf("changed acceptance contract replay error=%v", err)
	}
	edges.mu.Lock()
	for worktreeID, worktree := range edges.worktrees {
		worktree.WorktreeHeadCommit = strings.Repeat("2", 40)
		worktree.WorktreeCommitsAheadBase = 2
		worktree.WorktreeChangedPathCount = 2
		edges.worktrees[worktreeID] = worktree
	}
	for operationID, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeStatus {
			operation.Result.WorktreeHeadCommit = strings.Repeat("2", 40)
			operation.Result.WorktreeCommitsAheadBase = 2
			operation.Result.WorktreeChangedPathCount = 2
			edges.operations[operationID] = operation
		}
	}
	edges.mu.Unlock()
	stale, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(stale, `"acceptance_state":"reconciliation_required"`) || strings.Contains(stale, `"state":"accepted"`) || !strings.Contains(stale, `"git_evidence_state":"stale"`) || !strings.Contains(stale, `"reconciliation_reason":"acceptance_receipt_conflict"`) {
		t.Fatalf("changed live evidence status=%s err=%v", stale, err)
	}
	persisted, found, err := queue.Task(started.TaskID)
	if err != nil || !found || persisted.Workers[0].AcceptanceReceipt == nil || persisted.Workers[0].AcceptanceReceipt.HeadCommit != "1123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("changed evidence mutated receipt=%+v found=%v err=%v", persisted.Workers, found, err)
	}
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `","idempotency_key":"cleanup-stale-evidence-01"}`)); err == nil || !strings.Contains(err.Error(), "evidence changed") {
		t.Fatalf("cleanup accepted changed evidence: %v", err)
	}
	for _, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeCleanup {
			t.Fatal("cleanup operation was created for stale acceptance evidence")
		}
	}
}

func TestProjectTaskLegacyNoContractStaysPendingWithLiveEvidence(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-legacy-pending-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	server.WithEdgeStore(newProjectTaskEdgeStore()).WithWorkQueue(queue)
	startedOutput, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-legacy-pending-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(startedOutput), &started); err != nil {
		t.Fatal(err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) || !strings.Contains(status, `"git_evidence_known":true`) ||
		!strings.Contains(status, `"git_evidence_state":"observed"`) || strings.Contains(status, `"acceptance_state":"accepted"`) {
		t.Fatalf("legacy status=%s err=%v", status, err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found || task.AcceptanceContract != nil || task.Workers[0].AcceptanceReceipt != nil {
		t.Fatalf("legacy task=%+v found=%v err=%v", task, found, err)
	}
}

func TestProjectTaskGitEvidenceCriteriaNotMetStaysPending(t *testing.T) {
	server, turns := modelTurnServer(t)
	queue, err := workqueue.Open(workqueue.Config{Root: filepath.Join(t.TempDir(), "queue"), ControllerID: "mcp-task-git-evidence-pending-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	server.WithEdgeStore(newProjectTaskEdgeStore()).WithWorkQueue(queue)
	startedOutput, err := server.table["project_task_start"].handler(json.RawMessage(`{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"git-evidence-pending-0001","git_evidence_contract":{"version":1,"minimum_commits_ahead_per_worker":2,"minimum_changed_paths_per_worker":2}}`))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(startedOutput), &started); err != nil {
		t.Fatal(err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) ||
		!strings.Contains(status, `"acceptance_state":"pending"`) || !strings.Contains(status, `"git_evidence_state":"criteria_not_met"`) ||
		strings.Contains(status, `"git_evidence_state":"verified"`) || strings.Contains(status, `"state":"accepted"`) {
		t.Fatalf("status=%s err=%v", status, err)
	}
	task, found, err := queue.Task(started.TaskID)
	if err != nil || !found || task.Workers[0].AcceptanceReceipt != nil {
		t.Fatalf("criteria-miss receipt=%+v found=%v err=%v", task.Workers, found, err)
	}
}

func TestProjectTaskAcceptanceReceiptSurvivesCleanupMarkerCrashAndCallerReplay(t *testing.T) {
	server, turns := modelTurnServer(t)
	root := filepath.Join(t.TempDir(), "queue")
	queue, err := workqueue.Open(workqueue.Config{Root: root, ControllerID: "mcp-task-cleanup-replay-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	edges := newProjectTaskEdgeStore()
	server.WithEdgeStore(edges).WithWorkQueue(queue)
	request := `{"alias":"project","target":"parrot","goals":["Commit one focused change."],"timeout_seconds":600,"idempotency_key":"parallel-cleanup-receipt-01","git_evidence_contract":{"version":1,"minimum_commits_ahead_per_worker":1,"minimum_changed_paths_per_worker":1}}`
	startedOutput, err := server.table["project_task_start"].handler(json.RawMessage(request))
	if err != nil {
		t.Fatal(err)
	}
	var started projectTaskView
	if err := json.Unmarshal([]byte(startedOutput), &started); err != nil {
		t.Fatal(err)
	}
	if err := turns.CompleteRuntime(context.Background(), started.Workers[0].RuntimeID); err != nil {
		t.Fatal(err)
	}
	status, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(status, `"state":"acceptance_pending"`) || !strings.Contains(status, `"acceptance_state":"pending"`) ||
		!strings.Contains(status, `"git_evidence_state":"verified"`) || !strings.Contains(status, `"git_evidence_recorded_at":`) {
		t.Fatalf("status=%s err=%v", status, err)
	}

	markerDB, err := sql.Open("sqlite", filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markerDB.Exec(`CREATE TRIGGER fail_cleanup_marker BEFORE UPDATE OF worktree_cleaned ON task_workers BEGIN SELECT RAISE(ABORT,'injected receipt marker failure'); END`); err != nil {
		_ = markerDB.Close()
		t.Fatal(err)
	}
	firstCleanup := `{"task_id":"` + started.TaskID + `","idempotency_key":"caller-cleanup-first-0001"}`
	if _, err := server.table["project_task_cleanup"].handler(json.RawMessage(firstCleanup)); err == nil || !strings.Contains(err.Error(), "cleanup marker persistence failed") {
		_ = markerDB.Close()
		t.Fatalf("first cleanup error=%v", err)
	}
	if _, err := markerDB.Exec(`DROP TRIGGER fail_cleanup_marker`); err != nil {
		_ = markerDB.Close()
		t.Fatal(err)
	}
	if err := markerDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	queue, err = workqueue.Open(workqueue.Config{Root: root, ControllerID: "mcp-task-cleanup-replay-test"})
	if err != nil {
		t.Fatal(err)
	}
	server.WithWorkQueue(queue)
	beforeRetryStatusCalls := edges.worktreeStatusRequests
	secondCleanup := `{"task_id":"` + started.TaskID + `","idempotency_key":"caller-cleanup-second-0002"}`
	cleaned, err := server.table["project_task_cleanup"].handler(json.RawMessage(secondCleanup))
	if err != nil || !strings.Contains(cleaned, `"state":"acceptance_pending"`) || !strings.Contains(cleaned, `"cleaned":true`) ||
		!strings.Contains(cleaned, `"git_evidence_state":"verified"`) || !strings.Contains(cleaned, `"git_evidence_recorded_at":`) {
		t.Fatalf("cleanup replay=%s err=%v", cleaned, err)
	}
	if edges.worktreeStatusRequests != beforeRetryStatusCalls {
		t.Fatalf("cleanup replay re-polled removed worktree: status requests %d -> %d", beforeRetryStatusCalls, edges.worktreeStatusRequests)
	}
	cleanupOperations := 0
	for _, operation := range edges.operations {
		if operation.Kind == edge.OperationProjectWorktreeCleanup {
			cleanupOperations++
			if operation.Request.IdempotencyKey != projectTaskWorktreeCleanupOperationKey(started.TaskID, 0) {
				t.Fatalf("cleanup operation key was not task-derived: %q", operation.Request.IdempotencyKey)
			}
		}
	}
	if cleanupOperations != 1 {
		t.Fatalf("cleanup operation count=%d, want one server-derived task/worker operation", cleanupOperations)
	}
	statusAfterCleanup, err := server.table["project_task_status"].handler(json.RawMessage(`{"task_id":"` + started.TaskID + `"}`))
	if err != nil || !strings.Contains(statusAfterCleanup, `"state":"acceptance_pending"`) || !strings.Contains(statusAfterCleanup, `"acceptance_state":"pending"`) ||
		!strings.Contains(statusAfterCleanup, `"git_evidence_state":"verified"`) {
		t.Fatalf("status after worktree removal=%s err=%v", statusAfterCleanup, err)
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

func TestProjectTaskCleanupOperationKeyBindsTaskAndWorker(t *testing.T) {
	taskA := "tg_11111111111111111111111111111111"
	taskB := "tg_22222222222222222222222222222222"
	key := projectTaskWorktreeCleanupOperationKey(taskA, 0)
	if len(key) > 96 || !validProjectTaskCleanupIdempotencyKey(key) || key != projectTaskWorktreeCleanupOperationKey(taskA, 0) {
		t.Fatalf("cleanup key is not stable and Edge-bounded: %q", key)
	}
	if key == projectTaskWorktreeCleanupOperationKey(taskB, 0) || key == projectTaskWorktreeCleanupOperationKey(taskA, 1) {
		t.Fatal("cleanup keys collided across task identity or worker ordinal")
	}
	for _, invalid := range []string{"short", " caller-key-01", "caller/key-01", strings.Repeat("a", 97)} {
		if validProjectTaskCleanupIdempotencyKey(invalid) {
			t.Errorf("invalid caller replay key accepted: %q", invalid)
		}
	}
}

func TestProjectTaskGitAcceptanceContractIsDeterministicAndFailClosed(t *testing.T) {
	contract := &workqueue.TaskAcceptanceContract{Version: 1, MinimumCommitsAheadPerWorker: 2, MinimumChangedPathsPerWorker: 3}
	for _, test := range []struct {
		name         string
		contract     *workqueue.TaskAcceptanceContract
		clean        bool
		commitsAhead int
		changedPaths int
		wantAccepted bool
	}{
		{name: "criteria match", contract: contract, clean: true, commitsAhead: 2, changedPaths: 3, wantAccepted: true},
		{name: "contract absent stays manual review", clean: true, commitsAhead: 2, changedPaths: 3},
		{name: "dirty worktree remains pending", contract: contract, commitsAhead: 2, changedPaths: 3},
		{name: "too few commits remains pending", contract: contract, clean: true, commitsAhead: 1, changedPaths: 3},
		{name: "too few paths remains pending", contract: contract, clean: true, commitsAhead: 2, changedPaths: 2},
		{name: "unknown version fails closed", contract: &workqueue.TaskAcceptanceContract{Version: 2, MinimumCommitsAheadPerWorker: 2, MinimumChangedPathsPerWorker: 3}, clean: true, commitsAhead: 2, changedPaths: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := taskAcceptanceContractSatisfied(test.contract, test.clean, test.commitsAhead, test.changedPaths); got != test.wantAccepted {
				t.Fatalf("accepted=%v, want %v", got, test.wantAccepted)
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
	if view.State != "reconciliation_required" || len(view.Workers) != 1 || view.Workers[0].RuntimeState != "unknown" || view.Workers[0].AcceptanceState != "reconciliation_required" || view.Workers[0].ReconciliationReason != "control_plane_unavailable" {
		t.Fatalf("view without reconciliation stores=%+v", view)
	}
}

func TestProjectTaskToolsFailClosedWithoutRequiredStores(t *testing.T) {
	server, _ := modelTurnServer(t)
	for name, body := range map[string]string{
		"project_task_start":   `{"alias":"project","target":"parrot","goals":["goal"],"timeout_seconds":60,"idempotency_key":"parallel-missing-0001"}`,
		"project_task_status":  `{"task_id":"tg_11111111111111111111111111111111"}`,
		"project_task_list":    `{"alias":"project","target":"parrot"}`,
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
	for _, name := range []string{"project_task_start", "project_task_status", "project_task_list", "project_task_cancel", "project_task_cleanup"} {
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
