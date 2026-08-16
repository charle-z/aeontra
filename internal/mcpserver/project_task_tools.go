package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

const (
	projectTaskLeaseTTL          = 2 * time.Minute
	projectTaskReconcileInterval = 10 * time.Second
)

var errWorkQueueUnavailable = errors.New("durable work queue is not configured")

type edgeOperationIdempotencyLookup interface {
	OperationByIdempotency(string, edge.OperationKind, string) (edge.Operation, bool, error)
}

type projectTaskStartParams struct {
	Alias          string   `json:"alias"`
	Target         string   `json:"target"`
	Goals          []string `json:"goals"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type projectTaskIDParams struct {
	TaskID string `json:"task_id"`
}

type projectTaskCleanupParams struct {
	TaskID         string `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type projectTaskWorkerView struct {
	Ordinal          int             `json:"ordinal"`
	State            string          `json:"state"`
	LifecycleState   workqueue.State `json:"lifecycle_state"`
	RuntimeState     string          `json:"runtime_state,omitempty"`
	AcceptanceState  string          `json:"acceptance_state"`
	WorktreeID       string          `json:"worktree_id,omitempty"`
	WorkspaceID      string          `json:"workspace_id,omitempty"`
	RuntimeID        string          `json:"runtime_id,omitempty"`
	Branch           string          `json:"branch,omitempty"`
	BaseCommit       string          `json:"base_commit"`
	HeadCommit       string          `json:"head_commit,omitempty"`
	GitEvidenceKnown bool            `json:"git_evidence_known,omitempty"`
	Clean            *bool           `json:"clean,omitempty"`
	CommitsAheadBase *int            `json:"commits_ahead_base,omitempty"`
	ChangedPathCount *int            `json:"changed_path_count,omitempty"`
	Summary          string          `json:"summary,omitempty"`
}

type projectTaskView struct {
	TaskID         string                  `json:"task_id"`
	Alias          string                  `json:"alias"`
	Target         string                  `json:"target"`
	BaseCommit     string                  `json:"base_commit"`
	State          string                  `json:"state"`
	LifecycleState workqueue.TaskState     `json:"lifecycle_state"`
	WorkerCount    int                     `json:"worker_count"`
	Workers        []projectTaskWorkerView `json:"workers"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	Cleaned        bool                    `json:"cleaned,omitempty"`
}

func (s *Server) WithWorkQueue(store *workqueue.Store) *Server {
	s.workQueue = store
	return s
}

// StartProjectTaskCoordinator starts restart-safe lease maintenance and runtime reconciliation.
func (s *Server) StartProjectTaskCoordinator(parent context.Context) {
	if s == nil || s.workQueue == nil || s.modelTurns == nil || s.edgeOperations == nil || s.edgeDevices == nil {
		return
	}
	s.taskLifecycleMu.Lock()
	defer s.taskLifecycleMu.Unlock()
	if s.taskCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.taskCancel = cancel
	s.taskWG.Add(1)
	go func() {
		defer s.taskWG.Done()
		_ = s.reconcileProjectTasksOnce(ctx)
		ticker := time.NewTicker(projectTaskReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.reconcileProjectTasksOnce(ctx)
			}
		}
	}()
}

func (s *Server) StopProjectTaskCoordinator() {
	if s == nil {
		return
	}
	s.taskLifecycleMu.Lock()
	cancel := s.taskCancel
	s.taskCancel = nil
	s.taskLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
		s.taskWG.Wait()
	}
}

func (s *Server) addProjectTaskTools(projectSchema map[string]any) {
	startHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	readHints := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	cancelHints := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}
	cleanupHints := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}
	taskID := stringSchema("opaque durable task group id", `^tg_[a-f0-9]{32}$`, 35)
	s.addDirectTool(toolDef{
		Name: "project_task_start", Description: "Start or reuse one durable group of up to four stock Codex workers. Each worker receives one explicit bounded goal, one server-owned fenced Git worktree, one registered workspace and one independent model runtime; workers never share a writer checkout.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"],
			"goals":           map[string]any{"type": "array", "minItems": 1, "maxItems": workqueue.MaxTaskWorkers, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": modelturn.MaxGoalBodyBytes}},
			"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": int(modelturn.MaxTurnTTL / time.Second)},
			"idempotency_key": stringSchema("caller-generated key for this exact task group", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,117}$`, 118),
		}, []string{"alias", "target", "goals", "timeout_seconds", "idempotency_key"}), Version: "1", Annotations: startHints,
	}, s.handleProjectTaskStart)
	s.addDirectTool(toolDef{Name: "project_task_status", Description: "Reconcile and return one durable multiworker task without exposing leases, fences, paths, prompts or credentials.", InputSchema: closedObject(map[string]any{"task_id": taskID}, []string{"task_id"}), Version: "1", Annotations: readHints}, s.handleProjectTaskStatus)
	s.addDirectTool(toolDef{Name: "project_task_cancel", Description: "Request cancellation of every nonterminal worker in one durable task. Repeated cancellation is idempotent.", InputSchema: closedObject(map[string]any{"task_id": taskID}, []string{"task_id"}), Version: "1", Annotations: cancelHints}, s.handleProjectTaskCancel)
	s.addDirectTool(toolDef{Name: "project_task_cleanup", Description: "Remove only clean terminal worker worktrees after exact lease and fence validation. Worker branches and durable task evidence are retained.", InputSchema: closedObject(map[string]any{"task_id": taskID, "idempotency_key": stringSchema("caller-generated cleanup key", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`, 96)}, []string{"task_id", "idempotency_key"}), Version: "1", Annotations: cleanupHints}, s.handleProjectTaskCleanup)
}

func (s *Server) handleProjectTaskStart(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	if s.modelTurns == nil {
		return "", errModelTurnStoreUnavailable
	}
	if s.edgeOperations == nil || s.edgeDevices == nil || s.edgeWorkspaces == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params projectTaskStartParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	params.Alias = strings.ToLower(strings.TrimSpace(params.Alias))
	params.Target = strings.ToLower(strings.TrimSpace(params.Target))
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if len(params.Goals) < 1 || len(params.Goals) > workqueue.MaxTaskWorkers || params.TimeoutSeconds < 1 || time.Duration(params.TimeoutSeconds)*time.Second > modelturn.MaxTurnTTL || len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > 118 {
		return "", modelturn.ErrInvalidRequest
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil || !s.edgeDevices.DeviceActive(device.ID) {
		return "", errors.New("active edge target not found")
	}
	snapshotRequest := edge.OperationRequest{Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: params.IdempotencyKey + ":snapshot"}
	snapshot, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectSnapshot, snapshotRequest)
	if err == nil {
		snapshot, err = s.edgeOperations.WaitOperation(context.Background(), snapshot.ID, 180*time.Second)
	}
	if err != nil || snapshot.State != edge.OperationSucceeded || snapshot.Result.SnapshotHead == "" || snapshot.Result.ProjectAlias != params.Alias || snapshot.Result.ProjectTarget != params.Target {
		if err != nil {
			return "", err
		}
		return "", errors.New("project task base snapshot failed")
	}

	refs := make([]modelturn.RuntimeBodyReference, 0, len(params.Goals))
	hashes := make([]string, 0, len(params.Goals))
	for index, goal := range params.Goals {
		if len([]byte(goal)) == 0 || int64(len([]byte(goal))) > modelturn.MaxGoalBodyBytes || !utf8.ValidString(goal) || strings.TrimSpace(goal) == "" {
			discardTaskGoalRefs(s.modelTurns, refs)
			return "", modelturn.ErrInvalidRequest
		}
		body := projectTaskWorkerGoal(index, len(params.Goals), goal)
		if int64(len(body)) > modelturn.MaxGoalBodyBytes {
			discardTaskGoalRefs(s.modelTurns, refs)
			return "", modelturn.ErrBodyTooLarge
		}
		ref, stageErr := s.modelTurns.StageRuntimeGoal(context.Background(), body, modelturn.MaxTurnTTL)
		if stageErr != nil {
			discardTaskGoalRefs(s.modelTurns, refs)
			return "", stageErr
		}
		refs = append(refs, ref)
		hashes = append(hashes, ref.ContentDigest)
	}
	groupHash := projectTaskGroupHash(hashes)
	goalRefs := make([]string, len(refs))
	for index, ref := range refs {
		goalRefs[index] = ref.BodyRef
	}
	task, created, err := s.workQueue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: params.IdempotencyKey, Project: params.Alias, Target: params.Target, BaseCommit: snapshot.Result.SnapshotHead,
		GoalHash: groupHash, WorkerGoalHashes: hashes, WorkerGoalRefs: goalRefs, Pool: "edge." + params.Target + ".runtime", Profile: "codex.worker",
		WorkerCount: len(params.Goals), ExecutionTimeoutSeconds: params.TimeoutSeconds,
	})
	if err != nil || !created {
		discardTaskGoalRefs(s.modelTurns, refs)
	}
	if err != nil {
		return "", err
	}
	_ = s.reconcileProjectTask(context.Background(), task.ID, true)
	task, _, err = s.workQueue.Task(task.ID)
	return marshalToolValue(projectTaskPublicView(task, false), err)
}

func (s *Server) handleProjectTaskStatus(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	var params projectTaskIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	_ = s.reconcileProjectTask(context.Background(), params.TaskID, false)
	task, found, err := s.workQueue.Task(params.TaskID)
	if err != nil || !found {
		return "", errors.New("project task not found")
	}
	return marshalToolValue(s.projectTaskStatusView(context.Background(), task), nil)
}

func (s *Server) handleProjectTaskCancel(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	var params projectTaskIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	task, err := s.workQueue.CancelTask(params.TaskID)
	if err != nil {
		return "", err
	}
	_ = s.reconcileProjectTask(context.Background(), task.ID, false)
	task, _, err = s.workQueue.Task(task.ID)
	return marshalToolValue(projectTaskPublicView(task, false), err)
}

func (s *Server) handleProjectTaskCleanup(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	var params projectTaskCleanupParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	task, found, err := s.workQueue.Task(params.TaskID)
	if err != nil || !found {
		return "", errors.New("project task not found")
	}
	if task.State != workqueue.TaskCompleted && task.State != workqueue.TaskFailed && task.State != workqueue.TaskCancelled {
		return "", errors.New("project task is not terminal")
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(task.Target)
	if err != nil {
		return "", err
	}
	for _, worker := range task.Workers {
		if worker.WorktreeID == "" {
			continue
		}
		request := edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID, WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence, IdempotencyKey: fmt.Sprintf("%s:%d", params.IdempotencyKey, worker.Ordinal)}
		op, _, createErr := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeCleanup, request)
		if createErr != nil {
			return "", createErr
		}
		op, createErr = s.edgeOperations.WaitOperation(context.Background(), op.ID, 180*time.Second)
		if createErr != nil || op.State != edge.OperationSucceeded {
			if createErr != nil {
				return "", createErr
			}
			return "", errors.New("project task worktree cleanup failed: " + op.SafeCode)
		}
	}
	return marshalToolValue(projectTaskPublicView(task, true), nil)
}

func (s *Server) reconcileProjectTasksOnce(ctx context.Context) error {
	if s == nil || s.workQueue == nil {
		return errWorkQueueUnavailable
	}
	s.taskReconcileMu.Lock()
	defer s.taskReconcileMu.Unlock()
	if err := s.workQueue.RecoverExpired(); err != nil {
		return err
	}
	tasks, err := s.workQueue.Tasks(workqueue.MaxListResults)
	if err != nil {
		return err
	}
	var joined error
	for _, task := range tasks {
		if task.State == workqueue.TaskCompleted || task.State == workqueue.TaskFailed || task.State == workqueue.TaskCancelled {
			continue
		}
		joined = errors.Join(joined, s.reconcileProjectTaskUnlocked(ctx, task.ID, false))
	}
	return joined
}

func (s *Server) reconcileProjectTask(ctx context.Context, taskID string, wait bool) error {
	s.taskReconcileMu.Lock()
	defer s.taskReconcileMu.Unlock()
	if s.workQueue != nil {
		if err := s.workQueue.RecoverExpired(); err != nil {
			return err
		}
	}
	return s.reconcileProjectTaskUnlocked(ctx, taskID, wait)
}

func (s *Server) reconcileProjectTaskUnlocked(ctx context.Context, taskID string, wait bool) error {
	if s.workQueue == nil || s.modelTurns == nil || s.edgeOperations == nil || s.edgeDevices == nil || s.edgeWorkspaces == nil {
		return errors.New("project task coordinator is unavailable")
	}
	task, found, err := s.workQueue.Task(taskID)
	if err != nil || !found {
		return errors.New("project task not found")
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(task.Target)
	if err != nil || !s.edgeDevices.DeviceActive(device.ID) {
		return errors.New("project task edge target is inactive")
	}
	var joined error
	for ordinal := 0; ordinal < task.WorkerCount; ordinal++ {
		refreshed, present, readErr := s.workQueue.Task(task.ID)
		if readErr != nil || !present {
			return errors.New("project task disappeared during reconciliation")
		}
		worker := refreshed.Workers[ordinal]
		if worker.State == workqueue.StateSucceeded || worker.State == workqueue.StateFailed || worker.State == workqueue.StateCancelled {
			continue
		}
		if worker.State == workqueue.StateQueued {
			worker, readErr = s.workQueue.LeaseTaskWorker(task.ID, ordinal, s.projectTaskHolder(), projectTaskLeaseTTL)
			if readErr != nil {
				joined = errors.Join(joined, readErr)
				continue
			}
			refreshed, _, readErr = s.workQueue.Task(task.ID)
			if readErr != nil {
				joined = errors.Join(joined, readErr)
				continue
			}
			worker = refreshed.Workers[ordinal]
		}
		if worker.State != workqueue.StateLeased || worker.LeaseHolder != s.projectTaskHolder() {
			continue
		}
		joined = errors.Join(joined, s.reconcileProjectTaskWorker(ctx, refreshed, worker, device, wait))
	}
	return joined
}

func (s *Server) reconcileProjectTaskWorker(ctx context.Context, task workqueue.TaskGroup, worker workqueue.TaskWorker, device edge.Device, wait bool) error {
	if worker.WorktreeID != "" && worker.Attempt > 1 {
		if err := s.claimProjectTaskWorktree(ctx, task, worker, device, wait); err != nil {
			return err
		}
	}
	if worker.RuntimeID != "" {
		runtime, err := s.modelTurns.Runtime(ctx, worker.RuntimeID)
		if err != nil {
			return err
		}
		if worker.CancelRequested && runtime.State != modelturn.RuntimeStateCancelled {
			_ = s.modelTurns.CancelRuntime(ctx, worker.RuntimeID)
			runtime, _ = s.modelTurns.Runtime(ctx, worker.RuntimeID)
		}
		if outcome, summary, terminal := projectTaskRuntimeOutcome(runtime); terminal {
			_, err := s.workQueue.CompleteTaskWorker(task.ID, worker.Ordinal, worker.LeaseID, worker.Fence, workqueue.Result{Outcome: outcome, Summary: summary, ResultRef: runtime.ResultRef})
			return err
		}
		_, err = s.workQueue.Heartbeat(worker.JobID, worker.LeaseID, worker.Fence, projectTaskLeaseTTL)
		return err
	}
	if worker.CancelRequested {
		if worker.WorktreeID == "" {
			op, found, err := s.recoverProjectTaskWorktreeOperation(task, worker, device)
			if err != nil {
				return err
			}
			if found {
				if wait && op.State != edge.OperationSucceeded && op.State != edge.OperationFailed && op.State != edge.OperationCancelled {
					op, err = s.edgeOperations.WaitOperation(ctx, op.ID, 180*time.Second)
					if err != nil {
						return err
					}
				}
				if op.State != edge.OperationSucceeded && op.State != edge.OperationFailed && op.State != edge.OperationCancelled {
					return nil
				}
				if op.State == edge.OperationSucceeded {
					if op.Result.WorkFence != worker.Fence || op.Result.WorkLeaseID != worker.LeaseID {
						recovered := worker
						recovered.WorktreeID = op.Result.WorktreeID
						if err := s.claimProjectTaskWorktree(ctx, task, recovered, device, wait); err != nil {
							return err
						}
					}
					if _, err := s.workQueue.BindTaskWorker(workqueue.TaskWorkerBinding{TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, WorktreeID: op.Result.WorktreeID, WorkspaceID: op.Result.WorkspaceID}); err != nil {
						return err
					}
				}
			}
		}
		_, err := s.workQueue.CompleteTaskWorker(task.ID, worker.Ordinal, worker.LeaseID, worker.Fence, workqueue.Result{Outcome: workqueue.StateCancelled, Summary: "cancelled before runtime start"})
		return err
	}
	if _, err := s.workQueue.Heartbeat(worker.JobID, worker.LeaseID, worker.Fence, projectTaskLeaseTTL); err != nil {
		return err
	}

	if worker.WorktreeID == "" {
		op, err := s.projectTaskWorktreeOperation(ctx, task, worker, device, wait)
		if err != nil {
			return err
		}
		if op.State == edge.OperationFailed || op.State == edge.OperationCancelled {
			_, completeErr := s.workQueue.CompleteTaskWorker(task.ID, worker.Ordinal, worker.LeaseID, worker.Fence, workqueue.Result{Outcome: workqueue.StateFailed, Summary: "worktree provisioning failed"})
			return completeErr
		}
		if op.State != edge.OperationSucceeded {
			return nil
		}
		if op.Result.WorkFence != worker.Fence || op.Result.WorkLeaseID != worker.LeaseID {
			recovered := worker
			recovered.WorktreeID = op.Result.WorktreeID
			if err := s.claimProjectTaskWorktree(ctx, task, recovered, device, wait); err != nil {
				return err
			}
		}
		if _, err := s.workQueue.BindTaskWorker(workqueue.TaskWorkerBinding{TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, WorktreeID: op.Result.WorktreeID, WorkspaceID: op.Result.WorkspaceID}); err != nil {
			return err
		}
		refreshed, _, err := s.workQueue.Task(task.ID)
		if err != nil {
			return err
		}
		worker = refreshed.Workers[worker.Ordinal]
	}

	binding, err := s.edgeWorkspaces.ResolveWorkspace(worker.WorkspaceID)
	if err != nil || !validWorkspaceBinding(binding, worker.WorkspaceID) || binding.DeviceID != device.ID || binding.Mode != "dev" {
		return errors.New("project task workspace is not registered yet")
	}
	executionTTL := time.Duration(task.ExecutionTimeoutSeconds) * time.Second
	runtime, _, err := s.modelTurns.StartBoundRuntime(ctx, modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: worker.WorkspaceID, Controller: modelturn.ControllerRemoteEdge,
		GoalSummary: projectTaskGoalSummary(worker.GoalHash), GoalRef: worker.GoalRef, GoalDigest: worker.GoalHash,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest(task.ID + ":worker:" + fmt.Sprint(worker.Ordinal)),
		TTL:                  modelturn.RemoteRuntimeStartupTTL, ExecutionTTL: executionTTL,
	})
	if err != nil {
		return err
	}
	_, err = s.workQueue.BindTaskWorker(workqueue.TaskWorkerBinding{TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID, RuntimeID: runtime.RuntimeID})
	return err
}

func (s *Server) projectTaskWorktreeOperation(ctx context.Context, task workqueue.TaskGroup, worker workqueue.TaskWorker, device edge.Device, wait bool) (edge.Operation, error) {
	op, found, err := s.recoverProjectTaskWorktreeOperation(task, worker, device)
	if err != nil {
		return edge.Operation{}, err
	}
	if !found {
		request := edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", IdempotencyKey: projectTaskWorktreeOperationKey(task.ID, worker.Ordinal), WorktreeBaseCommit: task.BaseCommit, WorktreeRole: "writer", WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence}
		op, _, err = s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeCreate, request)
		if err != nil {
			return edge.Operation{}, err
		}
		if _, err := s.workQueue.BindTaskWorkerOperation(workqueue.TaskWorkerOperationBinding{TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, OperationID: op.ID}); err != nil {
			return edge.Operation{}, err
		}
	}
	if wait && op.State != edge.OperationSucceeded && op.State != edge.OperationFailed && op.State != edge.OperationCancelled {
		return s.edgeOperations.WaitOperation(ctx, op.ID, 180*time.Second)
	}
	return op, nil
}

func (s *Server) recoverProjectTaskWorktreeOperation(task workqueue.TaskGroup, worker workqueue.TaskWorker, device edge.Device) (edge.Operation, bool, error) {
	if worker.OperationID != "" {
		op, err := s.edgeOperations.OperationStatus(worker.OperationID)
		return op, err == nil, err
	}
	lookup, ok := s.edgeOperations.(edgeOperationIdempotencyLookup)
	if !ok {
		return edge.Operation{}, false, nil
	}
	op, found, err := lookup.OperationByIdempotency(device.ID, edge.OperationProjectWorktreeCreate, projectTaskWorktreeOperationKey(task.ID, worker.Ordinal))
	if err != nil || !found {
		return edge.Operation{}, found, err
	}
	if _, err := s.workQueue.BindTaskWorkerOperation(workqueue.TaskWorkerOperationBinding{TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, LeaseID: worker.LeaseID, Fence: worker.Fence, OperationID: op.ID}); err != nil {
		return edge.Operation{}, false, err
	}
	return op, true, nil
}

func projectTaskWorktreeOperationKey(taskID string, ordinal int) string {
	return taskID + ":worker:" + fmt.Sprint(ordinal)
}

func (s *Server) claimProjectTaskWorktree(ctx context.Context, task workqueue.TaskGroup, worker workqueue.TaskWorker, device edge.Device, wait bool) error {
	status, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID})
	if err != nil {
		return err
	}
	if wait {
		status, err = s.edgeOperations.WaitOperation(ctx, status.ID, 180*time.Second)
	} else {
		status, err = s.edgeOperations.WaitOperation(ctx, status.ID, 10*time.Second)
	}
	if err != nil || status.State != edge.OperationSucceeded {
		if err != nil {
			return err
		}
		return errors.New("project task worktree status failed: " + status.SafeCode)
	}
	if status.Result.WorkFence == worker.Fence && status.Result.WorkLeaseID == worker.LeaseID {
		return nil
	}
	claim, _, err := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeClaim, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID, WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence})
	if err != nil {
		return err
	}
	claim, err = s.edgeOperations.WaitOperation(ctx, claim.ID, 180*time.Second)
	if err != nil || claim.State != edge.OperationSucceeded {
		if err != nil {
			return err
		}
		return errors.New("project task worktree claim failed")
	}
	return nil
}

func projectTaskWorkerGoal(index, total int, goal string) []byte {
	return []byte(fmt.Sprintf("You are Codex worker %d of %d in a durable MCP Devbox task. Work only in your assigned isolated Git worktree and branch. Complete the assigned subtask, run focused tests, and create one reviewable commit using the repository convention. Do not inspect or modify sibling worktrees. Report the commit and verified result.\n\nAssigned subtask:\n%s", index+1, total, strings.TrimSpace(goal)))
}

func projectTaskGroupHash(hashes []string) string {
	sum := sha256.Sum256([]byte(strings.Join(hashes, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func projectTaskGoalSummary(digest string) string {
	if len(digest) < len("sha256:")+24 {
		return ""
	}
	return "goal:sha256:" + digest[len("sha256:"):len("sha256:")+24]
}

func (s *Server) projectTaskHolder() string {
	sum := sha256.Sum256([]byte(s.BootID()))
	return "task-coordinator-" + hex.EncodeToString(sum[:8])
}

func projectTaskRuntimeOutcome(runtime modelturn.Runtime) (workqueue.State, string, bool) {
	switch runtime.State {
	case modelturn.RuntimeStateCompleted:
		return workqueue.StateSucceeded, "runtime completed", true
	case modelturn.RuntimeStateCancelled:
		return workqueue.StateCancelled, "runtime cancelled", true
	case modelturn.RuntimeStateFailed:
		return workqueue.StateFailed, "runtime failed", true
	case modelturn.RuntimeStateExpired:
		return workqueue.StateFailed, "runtime expired", true
	default:
		return "", "", false
	}
}

func projectTaskPublicView(task workqueue.TaskGroup, cleaned bool) projectTaskView {
	view := projectTaskView{TaskID: task.ID, Alias: task.Project, Target: task.Target, BaseCommit: task.BaseCommit, State: projectTaskSemanticState(task.State), LifecycleState: task.State, WorkerCount: task.WorkerCount, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, Cleaned: cleaned, Workers: make([]projectTaskWorkerView, 0, len(task.Workers))}
	for _, worker := range task.Workers {
		branch := ""
		if strings.HasPrefix(worker.WorktreeID, "wt_") {
			branch = "codex/worktree-" + strings.TrimPrefix(worker.WorktreeID, "wt_")
		}
		state, runtimeState, acceptanceState := projectTaskWorkerSemanticState(worker)
		view.Workers = append(view.Workers, projectTaskWorkerView{Ordinal: worker.Ordinal, State: state, LifecycleState: worker.State, RuntimeState: runtimeState, AcceptanceState: acceptanceState, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID, RuntimeID: worker.RuntimeID, Branch: branch, BaseCommit: task.BaseCommit, Summary: worker.Summary})
	}
	return view
}

func projectTaskSemanticState(state workqueue.TaskState) string {
	switch state {
	case workqueue.TaskCompleted:
		return "acceptance_pending"
	case workqueue.TaskFailed:
		return "failed"
	case workqueue.TaskCancelled:
		return "cancelled"
	default:
		return "running"
	}
}

func projectTaskWorkerSemanticState(worker workqueue.TaskWorker) (string, string, string) {
	switch worker.State {
	case workqueue.StateSucceeded:
		return "acceptance_pending", string(modelturn.RuntimeStateCompleted), "pending"
	case workqueue.StateFailed:
		return "failed", string(modelturn.RuntimeStateFailed), "failed"
	case workqueue.StateCancelled:
		return "cancelled", string(modelturn.RuntimeStateCancelled), "cancelled"
	default:
		return "running", "", "not_ready"
	}
}

func (s *Server) projectTaskStatusView(ctx context.Context, task workqueue.TaskGroup) projectTaskView {
	view := projectTaskPublicView(task, false)
	if s.modelTurns == nil || s.edgeOperations == nil || s.edgeDevices == nil {
		for index := range view.Workers {
			if view.Workers[index].State == "acceptance_pending" {
				view.Workers[index].State = "reconciliation_required"
				view.Workers[index].RuntimeState = "unknown"
				view.Workers[index].AcceptanceState = "reconciliation_required"
			}
		}
		view.State = projectTaskViewSemanticState(view.Workers)
		return view
	}
	resolver, resolverOK := s.edgeDevices.(edgeDeviceAliasRegistry)
	device, deviceErr := edge.Device{}, errors.New("edge unavailable")
	if resolverOK {
		device, deviceErr = resolver.ResolveActiveDeviceName(task.Target)
		if deviceErr == nil && !s.edgeDevices.DeviceActive(device.ID) {
			deviceErr = errors.New("edge inactive")
		}
	}
	for index, worker := range task.Workers {
		item := &view.Workers[index]
		if worker.RuntimeID == "" {
			if worker.State == workqueue.StateCancelled {
				item.RuntimeState = string(modelturn.RuntimeStateCancelled)
			}
			continue
		}
		runtime, err := s.modelTurns.Runtime(ctx, worker.RuntimeID)
		if err != nil {
			item.State, item.RuntimeState, item.AcceptanceState = "reconciliation_required", "unknown", "reconciliation_required"
			continue
		}
		item.RuntimeState = string(runtime.State)
		switch runtime.State {
		case modelturn.RuntimeStateCompleted:
			if worker.State != workqueue.StateSucceeded || worker.WorktreeID == "" || deviceErr != nil {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				continue
			}
			status, _, statusErr := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID})
			if statusErr == nil {
				status, statusErr = s.edgeOperations.WaitOperation(ctx, status.ID, 10*time.Second)
			}
			result := status.Result
			if statusErr != nil || status.State != edge.OperationSucceeded || !result.WorktreeEvidenceKnown || result.WorktreeID != worker.WorktreeID ||
				result.WorktreeBaseCommit != task.BaseCommit || result.WorktreeBranch != item.Branch || result.WorktreeHeadCommit == "" {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				continue
			}
			clean, ahead, changed := result.WorktreeClean, result.WorktreeCommitsAheadBase, result.WorktreeChangedPathCount
			item.State, item.AcceptanceState = "acceptance_pending", "pending"
			item.GitEvidenceKnown, item.HeadCommit = true, result.WorktreeHeadCommit
			item.Clean, item.CommitsAheadBase, item.ChangedPathCount = &clean, &ahead, &changed
		case modelturn.RuntimeStateFailed, modelturn.RuntimeStateExpired:
			if worker.State == workqueue.StateFailed {
				item.State, item.AcceptanceState = "failed", "failed"
			} else {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
			}
		case modelturn.RuntimeStateCancelled:
			if worker.State == workqueue.StateCancelled {
				item.State, item.AcceptanceState = "cancelled", "cancelled"
			} else {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
			}
		default:
			if worker.State == workqueue.StateSucceeded || worker.State == workqueue.StateFailed || worker.State == workqueue.StateCancelled {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
			} else {
				item.State, item.AcceptanceState = "running", "not_ready"
			}
		}
	}
	view.State = projectTaskViewSemanticState(view.Workers)
	return view
}

func projectTaskViewSemanticState(workers []projectTaskWorkerView) string {
	allPending, anyFailed, anyReconciliation, anyRunning, anyCancelled := len(workers) > 0, false, false, false, false
	for _, worker := range workers {
		anyFailed = anyFailed || worker.State == "failed"
		anyReconciliation = anyReconciliation || worker.State == "reconciliation_required"
		anyRunning = anyRunning || worker.State == "running"
		anyCancelled = anyCancelled || worker.State == "cancelled"
		allPending = allPending && worker.State == "acceptance_pending"
	}
	if anyFailed {
		return "failed"
	}
	if anyReconciliation {
		return "reconciliation_required"
	}
	if anyRunning {
		return "running"
	}
	if allPending {
		return "acceptance_pending"
	}
	if anyCancelled {
		return "cancelled"
	}
	return "running"
}

func discardTaskGoalRefs(store *modelturn.Store, refs []modelturn.RuntimeBodyReference) {
	for _, ref := range refs {
		_ = store.DiscardRuntimeGoal(context.Background(), ref.BodyRef, ref.ContentDigest)
	}
}
