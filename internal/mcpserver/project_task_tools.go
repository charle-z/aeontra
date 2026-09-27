package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
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

var projectTaskTestProfileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type edgeOperationIdempotencyLookup interface {
	OperationByIdempotency(string, edge.OperationKind, string) (edge.Operation, bool, error)
}

type projectTaskStartParams struct {
	Alias               string                            `json:"alias"`
	Target              string                            `json:"target"`
	Goals               []string                          `json:"goals"`
	TimeoutSeconds      int                               `json:"timeout_seconds"`
	IdempotencyKey      string                            `json:"idempotency_key"`
	GitEvidenceContract *workqueue.TaskAcceptanceContract `json:"git_evidence_contract,omitempty"`
	TestProfileID       string                            `json:"test_profile_id,omitempty"`
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
	Ordinal                int                    `json:"ordinal"`
	State                  string                 `json:"state"`
	Attention              string                 `json:"attention,omitempty"`
	LifecycleState         workqueue.State        `json:"lifecycle_state"`
	RuntimeState           string                 `json:"runtime_state,omitempty"`
	AcceptanceState        string                 `json:"acceptance_state"`
	ReconciliationReason   string                 `json:"reconciliation_reason,omitempty"`
	LastRuntimePhase       modelturn.RuntimePhase `json:"last_runtime_phase,omitempty"`
	LastRuntimePhaseAt     *time.Time             `json:"last_runtime_phase_at,omitempty"`
	GitEvidenceState       string                 `json:"git_evidence_state,omitempty"`
	TestEvidenceState      string                 `json:"test_evidence_state,omitempty"`
	TestProfileID          string                 `json:"test_profile_id,omitempty"`
	WorktreeID             string                 `json:"worktree_id,omitempty"`
	WorkspaceID            string                 `json:"workspace_id,omitempty"`
	RuntimeID              string                 `json:"runtime_id,omitempty"`
	Branch                 string                 `json:"branch,omitempty"`
	BaseCommit             string                 `json:"base_commit"`
	HeadCommit             string                 `json:"head_commit,omitempty"`
	GitEvidenceKnown       bool                   `json:"git_evidence_known,omitempty"`
	Clean                  *bool                  `json:"clean,omitempty"`
	CommitsAheadBase       *int                   `json:"commits_ahead_base,omitempty"`
	ChangedPathCount       *int                   `json:"changed_path_count,omitempty"`
	GitEvidenceRecordedAt  *time.Time             `json:"git_evidence_recorded_at,omitempty"`
	TestEvidenceRecordedAt *time.Time             `json:"test_evidence_recorded_at,omitempty"`
	TurnSequence           uint64                 `json:"turn_sequence,omitempty"`
	ActiveTurnCreatedAt    *time.Time             `json:"active_turn_created_at,omitempty"`
	ModelWaitSeconds       *int64                 `json:"model_wait_seconds,omitempty"`
	Summary                string                 `json:"summary,omitempty"`
}

type projectTaskContinuation struct {
	State    string `json:"state"`
	NextTool string `json:"next_tool,omitempty"`
}

type projectTaskHandoff struct {
	Version      int    `json:"version"`
	Revision     string `json:"revision"`
	ResumePrompt string `json:"resume_prompt"`
}

type projectTaskView struct {
	TaskID               string                                `json:"task_id"`
	Alias                string                                `json:"alias"`
	Target               string                                `json:"target"`
	BaseCommit           string                                `json:"base_commit"`
	State                string                                `json:"state"`
	LifecycleState       workqueue.TaskState                   `json:"lifecycle_state"`
	WorkerCount          int                                   `json:"worker_count"`
	GitEvidenceContract  *workqueue.TaskAcceptanceContract     `json:"git_evidence_contract,omitempty"`
	TestEvidenceContract *workqueue.TaskTestAcceptanceContract `json:"test_evidence_contract,omitempty"`
	Continuation         *projectTaskContinuation              `json:"continuation,omitempty"`
	AttentionOrder       []int                                 `json:"attention_order,omitempty"`
	Handoff              *projectTaskHandoff                   `json:"handoff,omitempty"`
	Workers              []projectTaskWorkerView               `json:"workers"`
	CreatedAt            time.Time                             `json:"created_at"`
	UpdatedAt            time.Time                             `json:"updated_at"`
	Cleaned              bool                                  `json:"cleaned,omitempty"`
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
	gitEvidenceContract := closedObject(map[string]any{
		"version":                          map[string]any{"type": "integer", "minimum": 1, "maximum": 1},
		"minimum_commits_ahead_per_worker": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000},
		"minimum_changed_paths_per_worker": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000},
	}, []string{"version", "minimum_commits_ahead_per_worker", "minimum_changed_paths_per_worker"})
	s.addDirectTool(toolDef{
		Name: "project_task_start", Description: "Start or reuse one durable group of up to four stock Codex workers. Each worker receives one explicit bounded goal, one server-owned fenced Git worktree, one registered workspace and one independent model runtime; workers never share a writer checkout. Optional Git evidence or operator-owned test profile evidence is pinned at creation; neither proves natural-language goal satisfaction.",
		InputSchema: closedObject(map[string]any{
			"alias": projectSchema["alias"], "target": projectSchema["target"],
			"goals":                 map[string]any{"type": "array", "minItems": 1, "maxItems": workqueue.MaxTaskWorkers, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": modelturn.MaxGoalBodyBytes}},
			"timeout_seconds":       map[string]any{"type": "integer", "minimum": 1, "maximum": int(modelturn.MaxTurnTTL / time.Second)},
			"idempotency_key":       stringSchema("caller-generated key for this exact task group", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,117}$`, 118),
			"git_evidence_contract": gitEvidenceContract,
			"test_profile_id":       stringSchema("optional operator-owned Edge test profile; no caller command, environment or working directory", `^[a-z0-9][a-z0-9._-]{0,63}$`, 64),
		}, []string{"alias", "target", "goals", "timeout_seconds", "idempotency_key"}), Version: "3", Annotations: startHints,
	}, s.handleProjectTaskStart)
	s.addDirectTool(toolDef{Name: "project_task_status", Description: "Reconcile and return one durable multiworker task without exposing leases, fences, paths, prompts or credentials. Runtime completion stays separate from semantic acceptance. An opt-in Git evidence contract may produce a durable verified Git receipt for an exact task/worker/worktree/workspace/base/branch/lease/fence and clean committed-change predicate; `acceptance_state` remains pending because this contract does not evaluate tests or natural-language goals. Receipts are revalidated before cleanup and remain available afterward. Missing or stale evidence requires reconciliation.", InputSchema: closedObject(map[string]any{"task_id": taskID}, []string{"task_id"}), Version: "3", Annotations: readHints}, s.handleProjectTaskStatus)
	s.addDirectTool(toolDef{Name: "project_task_list", Description: "List up to 20 recent durable tasks for one project and Edge target, including retained terminal tasks, so a new chat can recover a lost task ID. This is local metadata only; use project_task_status for live reconciliation.", InputSchema: closedObject(map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"], "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, []string{"alias", "target"}), Version: "1", Annotations: readHints}, s.handleProjectTaskList)
	testWorkerSchema := closedObject(map[string]any{"task_id": taskID, "ordinal": map[string]any{"type": "integer", "minimum": 0, "maximum": workqueue.MaxTaskWorkers - 1}}, []string{"task_id", "ordinal"})
	s.addDirectTool(toolDef{Name: "project_task_test_start", Description: "Start or reuse the single operator-owned test profile pinned when this task was created, on one completed worker's exact managed worktree. The Edge runs it as a durable asynchronous process; no caller argv, environment or working directory is accepted. This does not accept the natural-language task goal.", InputSchema: testWorkerSchema, Version: "1", Annotations: startHints}, s.handleProjectTaskTestStart)
	s.addDirectTool(toolDef{Name: "project_task_test_status", Description: "Read one worker's durable test process and revalidate its pinned worktree, lease, fence, profile, source digest and exit status. A zero exit can record a bounded test receipt, but semantic task acceptance remains pending. This tool never stops a process.", InputSchema: testWorkerSchema, Version: "1", Annotations: readHints}, s.handleProjectTaskTestStatus)
	s.addDirectTool(toolDef{Name: "project_task_test_stop", Description: "Request a bounded stop of the test process owned by this exact task worker, including after the worker lease changes. Stopping never counts as a passed test.", InputSchema: testWorkerSchema, Version: "1", Annotations: cancelHints}, s.handleProjectTaskTestStop)
	s.addDirectTool(toolDef{Name: "project_task_cancel", Description: "Request cancellation of every nonterminal worker in one durable task. Repeated cancellation is idempotent.", InputSchema: closedObject(map[string]any{"task_id": taskID}, []string{"task_id"}), Version: "1", Annotations: cancelHints}, s.handleProjectTaskCancel)
	s.addDirectTool(toolDef{Name: "project_task_cleanup", Description: "Remove only terminal worker worktrees after exact lease and fence validation. A succeeded worker with an opt-in Git or test evidence contract requires a durable, revalidated receipt before cleanup; neither receipt is semantic goal acceptance. Worker branches and durable evidence remain. The caller idempotency key correlates retries; Edge cleanup identity is server-derived.", InputSchema: closedObject(map[string]any{"task_id": taskID, "idempotency_key": stringSchema("caller retry-correlation key; Edge operation identity is server-derived per task and worker", `^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`, 96)}, []string{"task_id", "idempotency_key"}), Version: "3", Annotations: cleanupHints}, s.handleProjectTaskCleanup)
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
	if len(params.Goals) < 1 || len(params.Goals) > workqueue.MaxTaskWorkers || params.TimeoutSeconds < 1 || time.Duration(params.TimeoutSeconds)*time.Second > modelturn.MaxTurnTTL || len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > 118 ||
		workqueue.ValidateTaskAcceptanceContract(params.GitEvidenceContract) != nil ||
		(params.TestProfileID != "" && !projectTaskTestProfileIDPattern.MatchString(params.TestProfileID)) ||
		(params.TestProfileID != "" && params.GitEvidenceContract != nil) {
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
	var testContract *workqueue.TaskTestAcceptanceContract
	if params.TestProfileID != "" {
		profileOperationKey, keyErr := projectTaskTestProfileOperationKey()
		if keyErr != nil {
			return "", keyErr
		}
		profile, _, profileErr := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeTestProfile, edge.OperationRequest{
			Alias: params.Alias, TargetAlias: params.Target, Profile: "linux-workcell", IdempotencyKey: profileOperationKey, TestProfileID: params.TestProfileID,
		})
		if profileErr == nil {
			profile, profileErr = s.edgeOperations.WaitOperation(context.Background(), profile.ID, 30*time.Second)
		}
		if profileErr != nil {
			return "", profileErr
		}
		candidate := &workqueue.TaskTestAcceptanceContract{Version: 1, ProfileID: profile.Result.TestProfileID, ProfileDigest: profile.Result.TestProfileDigest}
		if profile.State != edge.OperationSucceeded || candidate.ProfileID != params.TestProfileID || workqueue.ValidateTaskTestAcceptanceContract(candidate) != nil || profile.Result.TestTimeoutSeconds < 1 || profile.Result.TestTimeoutSeconds > 86400 {
			return "", errors.New("project task test profile is unavailable or invalid")
		}
		testContract = candidate
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
		GoalHash: groupHash, AcceptanceContract: params.GitEvidenceContract, TestAcceptanceContract: testContract, WorkerGoalHashes: hashes, WorkerGoalRefs: goalRefs, Pool: "edge." + params.Target + ".runtime", Profile: "codex.worker",
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

func projectTaskTestProfileOperationKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("project task test profile operation identity unavailable")
	}
	return "task-test-profile:" + hex.EncodeToString(nonce[:]), nil
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
		task.Pool != "edge."+params.Target+".runtime" || task.Profile != "codex.worker" || len(task.Workers) != len(hashes) ||
		!sameProjectTaskAcceptanceContract(task.AcceptanceContract, params.GitEvidenceContract) ||
		!sameProjectTaskTestProfile(task.TestAcceptanceContract, params.TestProfileID) {
		return false
	}
	for index, worker := range task.Workers {
		if worker.Ordinal != index || worker.GoalHash != hashes[index] {
			return false
		}
	}
	return true
}

func sameProjectTaskTestProfile(contract *workqueue.TaskTestAcceptanceContract, profileID string) bool {
	if contract == nil {
		return profileID == ""
	}
	return contract.ProfileID == profileID && workqueue.ValidateTaskTestAcceptanceContract(contract) == nil
}

func sameProjectTaskAcceptanceContract(left, right *workqueue.TaskAcceptanceContract) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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
			view.Workers[index].ReconciliationReason = "task_goal_unavailable"
			invalidGoalWorkers = append(invalidGoalWorkers, index)
			if worker.RuntimeID != "" {
				if runtime, runtimeErr := s.modelTurns.Runtime(context.Background(), worker.RuntimeID); runtimeErr == nil {
					view.Workers[index].RuntimeState = string(runtime.State)
				}
			}
		}
		view.State = "reconciliation_required"
		finalizeProjectTaskView(&view, time.Now().UTC())
		for _, index := range invalidGoalWorkers {
			view.Workers[index].Attention = "task_goal_unavailable"
		}
		setProjectTaskHandoff(&view)
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
	if !validProjectTaskCleanupIdempotencyKey(params.IdempotencyKey) {
		return "", errors.New("project task cleanup idempotency key is invalid")
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
		if worker.WorktreeID == "" || worker.WorktreeCleaned {
			continue
		}
		if task.TestAcceptanceContract != nil && worker.State == workqueue.StateSucceeded && worker.TestAcceptanceReceipt == nil {
			return "", errors.New("project task test evidence receipt is required before cleanup")
		}
		cleanupKey := projectTaskWorktreeCleanupOperationKey(task.ID, worker.Ordinal)
		request := edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID, WorkJobID: worker.JobID, WorkLeaseID: worker.LeaseID, WorkFence: worker.Fence, IdempotencyKey: cleanupKey}
		if task.AcceptanceContract != nil && worker.State == workqueue.StateSucceeded {
			if worker.AcceptanceReceipt == nil {
				return "", errors.New("project task Git evidence receipt is required before cleanup")
			}
			if !succeededTaskCleanupAlreadyExists(device.ID, s.edgeOperations, cleanupKey, request) {
				if err := s.revalidateTaskAcceptanceReceipt(context.Background(), task, worker, device.ID); err != nil {
					return "", err
				}
			}
		}
		if task.TestAcceptanceContract != nil && worker.State == workqueue.StateSucceeded && !succeededTaskCleanupAlreadyExists(device.ID, s.edgeOperations, cleanupKey, request) {
			status, statusErr := s.projectTaskTestStatus(context.Background(), projectTaskTestParams{TaskID: task.ID, Ordinal: worker.Ordinal})
			if statusErr != nil || status.TestEvidenceState != "verified" {
				return "", errors.New("project task test evidence changed or is unavailable")
			}
		}
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
		if err := s.workQueue.MarkTaskWorkerWorktreeCleaned(task.ID, worker.Ordinal, worker.LeaseID, worker.Fence); err != nil {
			return "", err
		}
	}
	updated, found, err := s.workQueue.Task(task.ID)
	if err != nil || !found {
		return "", errors.New("project task cleanup checkpoint unavailable")
	}
	cleaned := true
	for _, worker := range updated.Workers {
		cleaned = cleaned && (worker.WorktreeID == "" || worker.WorktreeCleaned)
	}
	return marshalToolValue(projectTaskPublicView(updated, cleaned), nil)
}

func validProjectTaskCleanupIdempotencyKey(key string) bool {
	if len(key) < 8 || len(key) > 96 || !((key[0] >= 'a' && key[0] <= 'z') || (key[0] >= 'A' && key[0] <= 'Z') || (key[0] >= '0' && key[0] <= '9')) {
		return false
	}
	for _, char := range key {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:-", char)) {
			return false
		}
	}
	return true
}

func projectTaskWorktreeCleanupOperationKey(taskID string, ordinal int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("project-task-cleanup:v1:%s:%d", taskID, ordinal)))
	return "task-cleanup:" + hex.EncodeToString(digest[:])
}

func succeededTaskCleanupAlreadyExists(deviceID string, operations edgeOperationRegistry, key string, request edge.OperationRequest) bool {
	lookup, ok := operations.(edgeOperationIdempotencyLookup)
	if !ok {
		return false
	}
	op, found, err := lookup.OperationByIdempotency(deviceID, edge.OperationProjectWorktreeCleanup, key)
	return err == nil && found && op.State == edge.OperationSucceeded && reflect.DeepEqual(op.Request, request)
}

func (s *Server) revalidateTaskAcceptanceReceipt(ctx context.Context, task workqueue.TaskGroup, worker workqueue.TaskWorker, deviceID string) error {
	receipt := worker.AcceptanceReceipt
	if receipt == nil || s.edgeOperations == nil {
		return errors.New("project task Git evidence receipt is unavailable")
	}
	status, _, err := s.edgeOperations.CreateOperation(deviceID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID})
	if err == nil {
		status, err = s.edgeOperations.WaitOperation(ctx, status.ID, 10*time.Second)
	}
	result := status.Result
	if err != nil || status.State != edge.OperationSucceeded || result.WorktreeState != "ready" || !result.WorktreeEvidenceKnown ||
		result.WorktreeID != receipt.WorktreeID || result.WorkspaceID != receipt.WorkspaceID || result.WorktreeRole != receipt.WorktreeRole ||
		result.WorktreeBaseCommit != receipt.BaseCommit || result.WorktreeBranch != receipt.Branch || result.WorktreeHeadCommit != receipt.HeadCommit ||
		result.WorkJobID != receipt.JobID || result.WorkLeaseID != receipt.LeaseID || result.WorkFence != receipt.Fence ||
		result.WorktreeClean != receipt.Clean || result.WorktreeCommitsAheadBase != receipt.CommitsAheadBase || result.WorktreeChangedPathCount != receipt.ChangedPathCount {
		return errors.New("project task Git evidence changed; cleanup requires reconciliation")
	}
	return nil
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
	view := projectTaskView{TaskID: task.ID, Alias: task.Project, Target: task.Target, BaseCommit: task.BaseCommit, State: projectTaskSemanticState(task.State), LifecycleState: task.State, WorkerCount: task.WorkerCount, GitEvidenceContract: task.AcceptanceContract, TestEvidenceContract: task.TestAcceptanceContract, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, Cleaned: cleaned, Workers: make([]projectTaskWorkerView, 0, len(task.Workers))}
	for _, worker := range task.Workers {
		branch := ""
		if strings.HasPrefix(worker.WorktreeID, "wt_") {
			branch = "codex/worktree-" + strings.TrimPrefix(worker.WorktreeID, "wt_")
		}
		state, runtimeState, acceptanceState := projectTaskWorkerSemanticState(worker)
		item := projectTaskWorkerView{Ordinal: worker.Ordinal, State: state, LifecycleState: worker.State, RuntimeState: runtimeState, AcceptanceState: acceptanceState, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID, RuntimeID: worker.RuntimeID, Branch: branch, BaseCommit: task.BaseCommit, Summary: worker.Summary}
		if task.TestAcceptanceContract != nil {
			item.TestProfileID = task.TestAcceptanceContract.ProfileID
			item.TestEvidenceState = "not_started"
		}
		if worker.WorktreeCleaned && worker.AcceptanceReceipt != nil {
			applyTaskAcceptanceReceipt(&item, *worker.AcceptanceReceipt)
		}
		if worker.WorktreeCleaned && worker.TestAcceptanceReceipt != nil {
			applyTaskTestReceipt(&item, *worker.TestAcceptanceReceipt)
		}
		view.Workers = append(view.Workers, item)
	}
	if task.State == workqueue.TaskCompleted {
		view.State = projectTaskViewSemanticState(view.Workers)
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

func applyTaskAcceptanceReceipt(view *projectTaskWorkerView, receipt workqueue.TaskAcceptanceReceipt) {
	view.State, view.AcceptanceState, view.GitEvidenceState = "acceptance_pending", "pending", "verified"
	view.GitEvidenceKnown, view.HeadCommit = true, receipt.HeadCommit
	clean, ahead, changed := receipt.Clean, receipt.CommitsAheadBase, receipt.ChangedPathCount
	view.Clean, view.CommitsAheadBase, view.ChangedPathCount = &clean, &ahead, &changed
	recordedAt := receipt.RecordedAt
	view.GitEvidenceRecordedAt = &recordedAt
}

func applyTaskTestReceipt(view *projectTaskWorkerView, receipt workqueue.TaskTestAcceptanceReceipt) {
	view.State, view.AcceptanceState, view.TestEvidenceState = "acceptance_pending", "pending", "verified"
	view.TestProfileID = receipt.ProfileID
	view.GitEvidenceKnown, view.HeadCommit = true, receipt.HeadCommit
	recordedAt := receipt.RecordedAt
	view.TestEvidenceRecordedAt = &recordedAt
}

func (s *Server) projectTaskStatusView(ctx context.Context, task workqueue.TaskGroup) projectTaskView {
	view := projectTaskPublicView(task, false)
	if s.modelTurns == nil || s.edgeOperations == nil || s.edgeDevices == nil {
		for index := range view.Workers {
			if view.Workers[index].State == "acceptance_pending" {
				view.Workers[index].State = "reconciliation_required"
				view.Workers[index].RuntimeState = "unknown"
				view.Workers[index].AcceptanceState = "reconciliation_required"
				view.Workers[index].ReconciliationReason = "control_plane_unavailable"
			}
		}
		view.State = projectTaskViewSemanticState(view.Workers)
		finalizeProjectTaskView(&view, time.Now().UTC())
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
		if worker.WorktreeCleaned {
			if worker.AcceptanceReceipt != nil {
				applyTaskAcceptanceReceipt(item, *worker.AcceptanceReceipt)
			} else if worker.TestAcceptanceReceipt != nil {
				applyTaskTestReceipt(item, *worker.TestAcceptanceReceipt)
			} else if worker.State == workqueue.StateSucceeded {
				item.State, item.RuntimeState, item.AcceptanceState = "reconciliation_required", string(modelturn.RuntimeStateCompleted), "reconciliation_required"
				item.ReconciliationReason = "acceptance_receipt_missing"
			}
			continue
		}
		if worker.RuntimeID == "" {
			if worker.State == workqueue.StateCancelled {
				item.RuntimeState = string(modelturn.RuntimeStateCancelled)
			}
			continue
		}
		runtime, err := s.modelTurns.Runtime(ctx, worker.RuntimeID)
		if err != nil {
			item.State, item.RuntimeState, item.AcceptanceState = "reconciliation_required", "unknown", "reconciliation_required"
			item.ReconciliationReason = "runtime_unavailable"
			continue
		}
		item.RuntimeState = string(runtime.State)
		item.TurnSequence = runtime.LastSequence
		item.ActiveTurnCreatedAt = runtime.ActiveTurnCreatedAt
		for _, phase := range runtime.Phases {
			at := phase.LastTimestamp
			if at.IsZero() {
				at = phase.Timestamp
			}
			if at.IsZero() || (item.LastRuntimePhaseAt != nil && !at.After(*item.LastRuntimePhaseAt)) {
				continue
			}
			item.LastRuntimePhase = phase.Phase
			item.LastRuntimePhaseAt = &at
		}
		switch runtime.State {
		case modelturn.RuntimeStateCompleted:
			if worker.State != workqueue.StateSucceeded || worker.WorktreeID == "" || deviceErr != nil {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				switch {
				case deviceErr != nil:
					item.ReconciliationReason = "edge_unavailable"
				case worker.State != workqueue.StateSucceeded:
					item.ReconciliationReason = "runtime_lifecycle_mismatch"
				case worker.WorktreeID == "":
					item.ReconciliationReason = "worktree_identity_missing"
				}
				continue
			}
			status, _, statusErr := s.edgeOperations.CreateOperation(device.ID, edge.OperationProjectWorktreeStatus, edge.OperationRequest{Alias: task.Project, TargetAlias: task.Target, Profile: "linux-workcell", WorktreeID: worker.WorktreeID})
			if statusErr == nil {
				status, statusErr = s.edgeOperations.WaitOperation(ctx, status.ID, 10*time.Second)
			}
			result := status.Result
			if statusErr != nil || status.State != edge.OperationSucceeded {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "worktree_status_unavailable"
				continue
			}
			if result.WorktreeState != "ready" || !result.WorktreeEvidenceKnown || result.WorktreeID != worker.WorktreeID ||
				result.WorkspaceID != worker.WorkspaceID || result.WorktreeRole != "writer" || result.WorkJobID != worker.JobID || result.WorkLeaseID != worker.LeaseID || result.WorkFence != worker.Fence ||
				result.WorktreeBaseCommit != task.BaseCommit || result.WorktreeBranch != item.Branch || !validProjectTaskCommit(result.WorktreeHeadCommit) ||
				result.WorktreeCommitsAheadBase < 0 || result.WorktreeCommitsAheadBase > 10000 || result.WorktreeChangedPathCount < 0 || result.WorktreeChangedPathCount > 10000 ||
				((result.WorktreeHeadCommit == result.WorktreeBaseCommit) != (result.WorktreeCommitsAheadBase == 0)) {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "worktree_evidence_mismatch"
				continue
			}
			clean, ahead, changed := result.WorktreeClean, result.WorktreeCommitsAheadBase, result.WorktreeChangedPathCount
			item.State, item.AcceptanceState = "acceptance_pending", "pending"
			item.GitEvidenceKnown, item.GitEvidenceState, item.HeadCommit = true, "observed", result.WorktreeHeadCommit
			item.Clean, item.CommitsAheadBase, item.ChangedPathCount = &clean, &ahead, &changed
			if taskAcceptanceContractSatisfied(task.AcceptanceContract, clean, ahead, changed) {
				candidate := workqueue.TaskAcceptanceReceipt{Version: 1, TaskID: task.ID, Ordinal: worker.Ordinal, JobID: worker.JobID, WorktreeID: worker.WorktreeID, WorkspaceID: worker.WorkspaceID, WorktreeRole: result.WorktreeRole, BaseCommit: result.WorktreeBaseCommit, HeadCommit: result.WorktreeHeadCommit, Branch: result.WorktreeBranch, LeaseID: worker.LeaseID, Fence: worker.Fence, ContractDigest: workqueue.TaskAcceptanceContractDigest(task.AcceptanceContract), Clean: clean, CommitsAheadBase: ahead, ChangedPathCount: changed}
				recorded, recordErr := s.workQueue.RecordTaskWorkerAcceptance(candidate)
				if recordErr != nil {
					item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
					item.ReconciliationReason = "acceptance_receipt_conflict"
					if worker.AcceptanceReceipt != nil {
						item.GitEvidenceState = "stale"
					}
					continue
				}
				applyTaskAcceptanceReceipt(item, recorded)
			} else if worker.AcceptanceReceipt != nil {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "git_evidence_stale"
				item.GitEvidenceState = "stale"
			} else if task.AcceptanceContract != nil {
				item.GitEvidenceState = "criteria_not_met"
			}
		case modelturn.RuntimeStateFailed, modelturn.RuntimeStateExpired:
			if worker.State == workqueue.StateFailed {
				item.State, item.AcceptanceState = "failed", "failed"
			} else {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "runtime_lifecycle_mismatch"
			}
		case modelturn.RuntimeStateCancelled:
			if worker.State == workqueue.StateCancelled {
				item.State, item.AcceptanceState = "cancelled", "cancelled"
			} else {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "runtime_lifecycle_mismatch"
			}
		default:
			if worker.State == workqueue.StateSucceeded || worker.State == workqueue.StateFailed || worker.State == workqueue.StateCancelled {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				item.ReconciliationReason = "runtime_lifecycle_mismatch"
			} else {
				item.State, item.AcceptanceState = "running", "not_ready"
				if runtime.State == modelturn.RuntimeStateDisconnected {
					item.ReconciliationReason = "runtime_disconnected"
				}
			}
		}
	}
	if task.TestAcceptanceContract != nil {
		for index, worker := range task.Workers {
			if worker.State != workqueue.StateSucceeded {
				continue
			}
			item := &view.Workers[index]
			testView, testErr := s.projectTaskTestStatus(ctx, projectTaskTestParams{TaskID: task.ID, Ordinal: worker.Ordinal})
			if testErr != nil {
				item.State, item.AcceptanceState, item.TestEvidenceState = "reconciliation_required", "reconciliation_required", "unavailable"
				item.ReconciliationReason = "test_evidence_unavailable"
				continue
			}
			item.TestEvidenceState = testView.TestEvidenceState
			item.TestProfileID = testView.ProfileID
			if testView.HeadCommit != "" {
				item.HeadCommit = testView.HeadCommit
			}
			if testView.TestEvidenceState == "stale" || testView.TestEvidenceState == "unavailable" {
				item.State, item.AcceptanceState = "reconciliation_required", "reconciliation_required"
				if testView.TestEvidenceState == "stale" {
					item.ReconciliationReason = "test_evidence_stale"
				} else {
					item.ReconciliationReason = "test_evidence_unavailable"
				}
			}
		}
	}
	view.State = projectTaskViewSemanticState(view.Workers)
	finalizeProjectTaskView(&view, time.Now().UTC())
	return view
}

// finalizeProjectTaskView adds a versioned, content-free handoff to a fresh
// status observation. It never changes a lease or grants a new writer.
func finalizeProjectTaskView(view *projectTaskView, now time.Time) {
	setProjectTaskContinuation(view)
	view.AttentionOrder = view.AttentionOrder[:0]
	for index := range view.Workers {
		worker := &view.Workers[index]
		worker.ModelWaitSeconds = nil
		if worker.Attention != "needs_model" {
			continue
		}
		view.AttentionOrder = append(view.AttentionOrder, index)
		if worker.ActiveTurnCreatedAt != nil {
			seconds := max(int64(0), int64(now.Sub(*worker.ActiveTurnCreatedAt)/time.Second))
			worker.ModelWaitSeconds = &seconds
		}
	}
	sort.Slice(view.AttentionOrder, func(left, right int) bool {
		a, b := view.Workers[view.AttentionOrder[left]].ActiveTurnCreatedAt, view.Workers[view.AttentionOrder[right]].ActiveTurnCreatedAt
		if a == nil || b == nil {
			if a == nil && b == nil {
				return view.AttentionOrder[left] < view.AttentionOrder[right]
			}
			return a == nil
		}
		if a.Equal(*b) {
			return view.AttentionOrder[left] < view.AttentionOrder[right]
		}
		return a.Before(*b)
	})
	for index, workerIndex := range view.AttentionOrder {
		view.AttentionOrder[index] = view.Workers[workerIndex].Ordinal
	}
	setProjectTaskHandoff(view)
}

func setProjectTaskHandoff(view *projectTaskView) {
	view.Handoff = &projectTaskHandoff{Version: 1, Revision: projectTaskCheckpointRevision(*view)}
	view.Handoff.ResumePrompt = "Continue Aeontra task " + view.TaskID + " from checkpoint " + view.Handoff.Revision + ". First call project_task_status for this task and use its current state; if the revision changed, discard this snapshot. Reconcile pending effects before retrying any action. Runtime completion is not objective acceptance."
}

func projectTaskCheckpointRevision(view projectTaskView) string {
	view.Handoff = nil
	view.AttentionOrder = nil
	view.Workers = append([]projectTaskWorkerView(nil), view.Workers...)
	for index := range view.Workers {
		view.Workers[index].Summary = ""
		view.Workers[index].ModelWaitSeconds = nil
	}
	payload, _ := json.Marshal(view) // This fixed view contains only JSON-supported fields.
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
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
		case worker.TestEvidenceState == "failed":
			worker.Attention = "inspect_failure"
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
	allPending, anyPending, anyFailed, anyReconciliation, anyRunning, anyCancelled := len(workers) > 0, false, false, false, false, false
	for _, worker := range workers {
		anyFailed = anyFailed || worker.State == "failed"
		anyReconciliation = anyReconciliation || worker.State == "reconciliation_required"
		anyRunning = anyRunning || worker.State == "running"
		anyCancelled = anyCancelled || worker.State == "cancelled"
		allPending = allPending && worker.State == "acceptance_pending"
		anyPending = anyPending || worker.State == "acceptance_pending"
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
	if allPending || (anyPending && !anyCancelled) {
		return "acceptance_pending"
	}
	if anyCancelled {
		return "cancelled"
	}
	return "running"
}

func taskAcceptanceContractSatisfied(contract *workqueue.TaskAcceptanceContract, clean bool, commitsAhead, changedPaths int) bool {
	return contract != nil && workqueue.ValidateTaskAcceptanceContract(contract) == nil && clean &&
		commitsAhead >= contract.MinimumCommitsAheadPerWorker && changedPaths >= contract.MinimumChangedPathsPerWorker
}

func validProjectTaskCommit(commit string) bool {
	decoded, err := hex.DecodeString(commit)
	return len(commit) == 40 && err == nil && strings.ToLower(commit) == commit && len(decoded) == 20
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
