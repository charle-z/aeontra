package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

type projectTaskListParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
	Limit  int    `json:"limit,omitempty"`
}

type projectTaskListItem struct {
	TaskID         string              `json:"task_id"`
	LifecycleState workqueue.TaskState `json:"lifecycle_state"`
	BaseCommit     string              `json:"base_commit"`
	WorkerCount    int                 `json:"worker_count"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type projectTaskListView struct {
	Alias    string                `json:"alias"`
	Target   string                `json:"target"`
	Tasks    []projectTaskListItem `json:"tasks"`
	NextTool string                `json:"next_tool,omitempty"`
}

type projectTaskCleanupParams struct {
	TaskID         string `json:"task_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type projectTaskWorkerView struct {
	Ordinal          int             `json:"ordinal"`
	State            string          `json:"state"`
	Attention        string          `json:"attention,omitempty"`
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

type projectTaskContinuation struct {
	State    string `json:"state"`
	NextTool string `json:"next_tool,omitempty"`
}

type projectTaskView struct {
	TaskID         string                   `json:"task_id"`
	Alias          string                   `json:"alias"`
	Target         string                   `json:"target"`
	BaseCommit     string                   `json:"base_commit"`
	State          string                   `json:"state"`
	LifecycleState workqueue.TaskState      `json:"lifecycle_state"`
	WorkerCount    int                      `json:"worker_count"`
	Continuation   *projectTaskContinuation `json:"continuation,omitempty"`
	Workers        []projectTaskWorkerView  `json:"workers"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	Cleaned        bool                     `json:"cleaned,omitempty"`
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
	s.addDirectTool(toolDef{Name: "project_task_list", Description: "List up to 20 recent durable tasks for one project and Edge target, including retained terminal tasks, so a new chat can recover a lost task ID. This is local metadata only; use project_task_status for live reconciliation.", InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"], "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, []string{"alias", "target"}), Version: "1", Annotations: readHints}, s.handleProjectTaskList)
	s.addDirectTool(toolDef{Name: "project_task_cancel", Description: "Request cancellation of every nonterminal worker in one durable task. Repeated cancellation is idempotent.", InputSchema: closedObject(map[string]any{"task_id": taskID}, []string{"task_id"}), Version: "1", Annotations: cancelHints}, s.handleProjectTaskCancel)
	s.addDirectTool(toolDef{Name: "project_task_cleanup", Description: "Remove only clean terminal worker worktrees after exact lease and fence validation. Worker branches and durable task evidence are retained.", InputSchema: closedObject(map[string]any{"task_id": taskID, "idempotency_key": stringSchema("caller-generated cleanup key", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`, 96)}, []string{"task_id", "idempotency_key"}), Version: "1", Annotations: cleanupHints}, s.handleProjectTaskCleanup)
}

func (s *Server) handleProjectTaskStart(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
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
	bodies, hashes, err := projectTaskGoalBodies(params.Goals)
	if err != nil {
		return "", err
	}
	groupHash := projectTaskGroupHash(hashes)
	startLock := s.projectTaskStartLock(params.IdempotencyKey)
	startLock.Lock()
	locked := true
	defer func() {
		if locked {
			startLock.Unlock()
		}
	}()
	if existing, found, lookupErr := s.workQueue.TaskByIdempotencyKey(params.IdempotencyKey); lookupErr != nil {
		return "", lookupErr
	} else if found {
		if !projectTaskRequestMatches(existing, params, hashes, groupHash) {
			return "", errors.New("workqueue: task idempotency key conflicts")
		}
		if !terminalProjectTask(existing.State) {
			if s.modelTurns == nil {
				return "", errModelTurnStoreUnavailable
			}
			if err := s.modelTurns.PinTaskGoalReferences(context.Background(), modelturn.IdempotencyDigest(existing.IdempotencyKey), taskGoalReferences(existing.Workers)); err != nil {
				return "", err
			}
		}
		return marshalToolValue(projectTaskPublicView(existing, false), nil)
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

	refs := make([]modelturn.RuntimeBodyReference, 0, len(bodies))
	for _, body := range bodies {
		ref, stageErr := s.modelTurns.StageRuntimeGoal(context.Background(), body, modelturn.MaxTurnTTL)
		if stageErr != nil {
			return "", errors.Join(stageErr, discardTaskGoalRefs(s.modelTurns, refs))
		}
		refs = append(refs, ref)
	}
	goalPins := runtimeTaskGoalReferences(refs)
	ownerDigest := modelturn.IdempotencyDigest(params.IdempotencyKey)
	if err := s.modelTurns.PinTaskGoalReferences(context.Background(), ownerDigest, goalPins); err != nil {
		return "", errors.Join(err, discardTaskGoalRefs(s.modelTurns, refs))
	}
	goalRefs := make([]string, len(refs))
	for index, ref := range refs {
		goalRefs[index] = ref.BodyRef
	}
	task, created, err := s.workQueue.CreateTask(workqueue.TaskSpec{
		IdempotencyKey: params.IdempotencyKey, Project: params.Alias, Target: params.Target, BaseCommit: snapshot.Result.SnapshotHead,
		GoalHash: groupHash, WorkerGoalHashes: hashes, WorkerGoalRefs: goalRefs, Pool: "edge." + params.Target + ".runtime", Profile: "codex.worker",
		WorkerCount: len(params.Goals), ExecutionTimeoutSeconds: params.TimeoutSeconds,
	})
	if err != nil {
		cleanupErr := s.modelTurns.UnpinTaskGoalReferences(context.Background(), ownerDigest, goalPins)
		return "", errors.Join(err, cleanupErr, discardTaskGoalRefs(s.modelTurns, refs))
	}
	if !created {
		if !projectTaskRequestMatches(task, params, hashes, groupHash) {
			cleanupErr := s.modelTurns.UnpinTaskGoalReferences(context.Background(), ownerDigest, goalPins)
			return "", errors.Join(errors.New("workqueue: task idempotency key conflicts"), cleanupErr, discardTaskGoalRefs(s.modelTurns, refs))
		}
		cleanupErr := s.modelTurns.UnpinTaskGoalReferences(context.Background(), ownerDigest, goalPins)
		if cleanupErr != nil {
			return "", cleanupErr
		}
		if err := s.modelTurns.PinTaskGoalReferences(context.Background(), ownerDigest, taskGoalReferences(task.Workers)); err != nil {
			return "", err
		}
		if err := discardTaskGoalRefs(s.modelTurns, refs); err != nil {
			return "", err
		}
	}
	startLock.Unlock()
	locked = false
	_ = s.reconcileProjectTask(context.Background(), task.ID, true)
	task, _, err = s.workQueue.Task(task.ID)
	return marshalToolValue(projectTaskPublicView(task, false), err)
}

func projectTaskGoalBodies(goals []string) ([][]byte, []string, error) {
	bodies := make([][]byte, len(goals))
	hashes := make([]string, len(goals))
	for index, goal := range goals {
		if len([]byte(goal)) == 0 || int64(len([]byte(goal))) > modelturn.MaxGoalBodyBytes || !utf8.ValidString(goal) || strings.TrimSpace(goal) == "" {
			return nil, nil, modelturn.ErrInvalidRequest
		}
		body := projectTaskWorkerGoal(index, len(goals), goal)
		if int64(len(body)) > modelturn.MaxGoalBodyBytes {
			return nil, nil, modelturn.ErrBodyTooLarge
		}
		sum := sha256.Sum256(body)
		bodies[index] = body
		hashes[index] = "sha256:" + hex.EncodeToString(sum[:])
	}
	return bodies, hashes, nil
}

func projectTaskRequestMatches(task workqueue.TaskGroup, params projectTaskStartParams, hashes []string, groupHash string) bool {
	if task.IdempotencyKey != params.IdempotencyKey || task.Project != params.Alias || task.Target != params.Target ||
		task.BaseCommit == "" || task.GoalHash != groupHash || task.WorkerCount != len(hashes) || task.ExecutionTimeoutSeconds != params.TimeoutSeconds ||
		task.Pool != "edge."+params.Target+".runtime" || task.Profile != "codex.worker" || len(task.Workers) != len(hashes) {
		return false
	}
	for index, worker := range task.Workers {
		if worker.Ordinal != index || worker.GoalHash != hashes[index] {
			return false
		}
	}
	return true
}

func (s *Server) projectTaskStartLock(key string) *sync.Mutex {
	digest := sha256.Sum256([]byte(key))
	return &s.taskStartLocks[int(digest[0])%len(s.taskStartLocks)]
}

func (s *Server) handleProjectTaskStatus(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	var params projectTaskIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	reconcileErr := s.reconcileProjectTask(context.Background(), params.TaskID, false)
	task, found, err := s.workQueue.Task(params.TaskID)
	if err != nil || !found {
		return "", errors.New("project task not found")
	}
	if errors.Is(reconcileErr, modelturn.ErrRequestRefConflict) {
		view := projectTaskPublicView(task, false)
		ownerDigest := modelturn.IdempotencyDigest(task.IdempotencyKey)
		invalidGoalWorkers := make([]int, 0, 1)
		for index, worker := range task.Workers {
			if worker.State == workqueue.StateSucceeded || worker.State == workqueue.StateFailed || worker.State == workqueue.StateCancelled {
				continue
			}
			ref := modelturn.TaskGoalReference{BodyRef: worker.GoalRef, ContentDigest: worker.GoalHash}
			if err := s.modelTurns.PinTaskGoalReferences(context.Background(), ownerDigest, []modelturn.TaskGoalReference{ref}); !errors.Is(err, modelturn.ErrRequestRefConflict) {
				continue
			}
			view.Workers[index].State = "reconciliation_required"
			view.Workers[index].AcceptanceState = "reconciliation_required"
			view.Workers[index].RuntimeState = "unknown"
			invalidGoalWorkers = append(invalidGoalWorkers, index)
			if worker.RuntimeID != "" {
				if runtime, runtimeErr := s.modelTurns.Runtime(context.Background(), worker.RuntimeID); runtimeErr == nil {
					view.Workers[index].RuntimeState = string(runtime.State)
				}
			}
		}
		view.State = "reconciliation_required"
		setProjectTaskContinuation(&view)
		for _, index := range invalidGoalWorkers {
			view.Workers[index].Attention = "task_goal_unavailable"
		}
		return marshalToolValue(view, nil)
	}
	return marshalToolValue(s.projectTaskStatusView(context.Background(), task), nil)
}

func (s *Server) handleProjectTaskList(arguments json.RawMessage) (string, error) {
	if s.workQueue == nil {
		return "", errWorkQueueUnavailable
	}
	var params projectTaskListParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	params.Alias = strings.ToLower(strings.TrimSpace(params.Alias))
	params.Target = strings.ToLower(strings.TrimSpace(params.Target))
	if params.Limit == 0 {
		params.Limit = 10
	}
	if params.Limit < 1 || params.Limit > 20 {
		return "", errors.New("project task list limit is invalid")
	}
	tasks, err := s.workQueue.RecentProjectTasks(params.Alias, params.Target, params.Limit)
	if err != nil {
		return "", err
	}
	view := projectTaskListView{Alias: params.Alias, Target: params.Target, Tasks: make([]projectTaskListItem, 0, len(tasks))}
	for _, task := range tasks {
		view.Tasks = append(view.Tasks, projectTaskListItem{TaskID: task.ID, LifecycleState: task.State, BaseCommit: task.BaseCommit, WorkerCount: task.WorkerCount, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt})
	}
	if len(view.Tasks) > 0 {
		view.NextTool = "project_task_status"
	}
	return marshalToolValue(view, nil)
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
	if err := s.reconcileProjectTaskGoalPins(ctx); err != nil {
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
	if !terminalProjectTask(task.State) {
		if err := s.modelTurns.PinTaskGoalReferences(ctx, modelturn.IdempotencyDigest(task.IdempotencyKey), taskGoalReferences(task.Workers)); err != nil {
			return err
		}
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

func (s *Server) reconcileProjectTaskGoalPins(ctx context.Context) error {
	if s.workQueue == nil || s.modelTurns == nil {
		return errModelTurnStoreUnavailable
	}
	refs, err := s.workQueue.ActiveTaskGoalRefs()
	if err != nil {
		return err
	}
	validByDigest := make(map[string]*modelturn.TaskGoalOwner)
	for _, ref := range refs {
		ownerDigest := modelturn.IdempotencyDigest(ref.IdempotencyKey)
		goalRef := modelturn.TaskGoalReference{BodyRef: ref.BodyRef, ContentDigest: ref.ContentDigest}
		if err := s.modelTurns.PinTaskGoalReferences(ctx, ownerDigest, []modelturn.TaskGoalReference{goalRef}); err != nil {
			if !errors.Is(err, modelturn.ErrRequestRefConflict) {
				return err
			}
			task, found, lookupErr := s.workQueue.TaskByIdempotencyKey(ref.IdempotencyKey)
			if lookupErr != nil {
				return lookupErr
			}
			if found {
				for _, worker := range task.Workers {
					if worker.GoalRef != ref.BodyRef || worker.GoalHash != ref.ContentDigest || (worker.State != workqueue.StateQueued && worker.State != workqueue.StateBlocked) || worker.RuntimeID != "" {
						continue
					}
					if _, err := s.workQueue.FailUnstartedTaskWorker(task.ID, worker.Ordinal); err != nil {
						return err
					}
					break
				}
			}
			continue
		}
		owner := validByDigest[ownerDigest]
		if owner == nil {
			owner = &modelturn.TaskGoalOwner{OwnerDigest: ownerDigest}
			validByDigest[ownerDigest] = owner
		}
		owner.References = append(owner.References, goalRef)
	}
	owners := make([]modelturn.TaskGoalOwner, 0, len(validByDigest))
	for _, owner := range validByDigest {
		owners = append(owners, *owner)
	}
	return s.modelTurns.ReconcileTaskGoalPins(ctx, owners, modelturn.TaskGoalPinOrphanGrace)
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
		setProjectTaskContinuation(&view)
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
	setProjectTaskContinuation(&view)
	return view
}

// setProjectTaskContinuation gives a new client a bounded resumption hint from
// durable state. It does not select or restrict the model's tools, create a new
// runtime, retry an effect, or assert that a completed worker met its goal.
func setProjectTaskContinuation(view *projectTaskView) {
	state := "wait"
	for index := range view.Workers {
		worker := &view.Workers[index]
		switch {
		case worker.State == "reconciliation_required" || worker.RuntimeState == string(modelturn.RuntimeStateDisconnected):
			worker.Attention = "reconcile"
		case worker.State == "failed":
			worker.Attention = "inspect_failure"
		case worker.State == "cancelled":
			worker.Attention = "none"
		case worker.State == "acceptance_pending":
			worker.Attention = "review_required"
		case worker.RuntimeState == string(modelturn.RuntimeStateAwaitingModel):
			worker.Attention = "needs_model"
		default:
			worker.Attention = "wait"
		}
		switch worker.Attention {
		case "reconcile":
			state = "reconcile"
		case "inspect_failure":
			if state != "reconcile" {
				state = "inspect_failure"
			}
		case "needs_model":
			if state != "reconcile" && state != "inspect_failure" {
				state = "needs_model"
			}
		case "review_required":
			if state == "wait" {
				state = "review_required"
			}
		}
	}
	if view.State == "cancelled" && state == "wait" {
		state = "none"
	}
	view.Continuation = &projectTaskContinuation{State: state}
	if state == "needs_model" {
		view.Continuation.NextTool = "model_turn_next"
	}
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

func terminalProjectTask(state workqueue.TaskState) bool {
	return state == workqueue.TaskCompleted || state == workqueue.TaskFailed || state == workqueue.TaskCancelled
}

func taskGoalReferences(workers []workqueue.TaskWorker) []modelturn.TaskGoalReference {
	refs := make([]modelturn.TaskGoalReference, 0, len(workers))
	for _, worker := range workers {
		if worker.State == workqueue.StateSucceeded || worker.State == workqueue.StateFailed || worker.State == workqueue.StateCancelled {
			continue
		}
		refs = append(refs, modelturn.TaskGoalReference{BodyRef: worker.GoalRef, ContentDigest: worker.GoalHash})
	}
	return refs
}

func runtimeTaskGoalReferences(refs []modelturn.RuntimeBodyReference) []modelturn.TaskGoalReference {
	result := make([]modelturn.TaskGoalReference, 0, len(refs))
	for _, ref := range refs {
		result = append(result, modelturn.TaskGoalReference{BodyRef: ref.BodyRef, ContentDigest: ref.ContentDigest})
	}
	return result
}

func discardTaskGoalRefs(store *modelturn.Store, refs []modelturn.RuntimeBodyReference) error {
	var joined error
	for _, ref := range refs {
		joined = errors.Join(joined, store.DiscardRuntimeGoal(context.Background(), ref.BodyRef, ref.ContentDigest))
	}
	return joined
}
