package workqueue

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MaxTaskWorkers = 4

type TaskState string

const (
	TaskQueued     TaskState = "queued"
	TaskRunning    TaskState = "running"
	TaskCancelling TaskState = "cancelling"
	TaskCompleted  TaskState = "completed"
	TaskFailed     TaskState = "failed"
	TaskCancelled  TaskState = "cancelled"
)

var (
	taskIDPattern        = regexp.MustCompile(`^tg_[a-f0-9]{32}$`)
	taskProjectPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	taskTargetPattern    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
	taskCommitPattern    = regexp.MustCompile(`^[a-f0-9]{40}$`)
	taskWorktreePattern  = regexp.MustCompile(`^wt_[a-f0-9]{32}$`)
	taskWorkspacePattern = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)
	taskRuntimePattern   = regexp.MustCompile(`^mr_[a-f0-9]{32}$`)
	taskGoalRefPattern   = regexp.MustCompile(`^mb_[a-f0-9]{32}$`)
	taskOperationPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
)

type TaskSpec struct {
	IdempotencyKey          string
	Project                 string
	Target                  string
	BaseCommit              string
	GoalHash                string
	WorkerGoalHashes        []string
	WorkerGoalRefs          []string
	Pool                    string
	Profile                 string
	WorkerCount             int
	ExecutionTimeoutSeconds int
}

type TaskGroup struct {
	ID                      string       `json:"task_id"`
	IdempotencyKey          string       `json:"-"`
	Project                 string       `json:"project"`
	Target                  string       `json:"target"`
	BaseCommit              string       `json:"base_commit"`
	GoalHash                string       `json:"-"`
	Pool                    string       `json:"pool"`
	Profile                 string       `json:"profile"`
	WorkerCount             int          `json:"worker_count"`
	ExecutionTimeoutSeconds int          `json:"execution_timeout_seconds"`
	State                   TaskState    `json:"state"`
	Workers                 []TaskWorker `json:"workers"`
	CreatedAt               time.Time    `json:"created_at"`
	UpdatedAt               time.Time    `json:"updated_at"`
}

type TaskWorker struct {
	Ordinal         int       `json:"ordinal"`
	JobID           string    `json:"job_id"`
	State           State     `json:"state"`
	Reason          Reason    `json:"reason,omitempty"`
	CancelRequested bool      `json:"cancel_requested"`
	Attempt         int       `json:"attempt"`
	LeaseID         string    `json:"lease_id,omitempty"`
	Fence           uint64    `json:"fence"`
	LeaseExpiresAt  time.Time `json:"lease_expires_at,omitempty"`
	LeaseHolder     string    `json:"-"`
	OperationID     string    `json:"-"`
	WorktreeID      string    `json:"worktree_id,omitempty"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	RuntimeID       string    `json:"runtime_id,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	GoalHash        string    `json:"-"`
	GoalRef         string    `json:"-"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type TaskWorkerBinding struct {
	TaskID      string
	Ordinal     int
	JobID       string
	LeaseID     string
	Fence       uint64
	WorktreeID  string
	WorkspaceID string
	RuntimeID   string
}

type TaskWorkerOperationBinding struct {
	TaskID      string
	Ordinal     int
	JobID       string
	LeaseID     string
	Fence       uint64
	OperationID string
}

func (s *Store) CreateTask(spec TaskSpec) (TaskGroup, bool, error) {
	spec = normalizeTaskSpec(spec)
	if s == nil || s.db == nil || validateTaskSpec(spec) != nil {
		return TaskGroup{}, false, errors.New("workqueue: task specification is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task transaction failed")
	}
	defer tx.Rollback()
	if existingID, found, err := taskIDByIdempotency(tx, spec.IdempotencyKey); err != nil {
		return TaskGroup{}, false, err
	} else if found {
		existing, found, err := taskByIDTx(tx, existingID)
		if err != nil || !found {
			return TaskGroup{}, false, errors.New("workqueue: task unavailable")
		}
		if !taskMatchesSpec(existing, spec) {
			return TaskGroup{}, false, errors.New("workqueue: task idempotency key conflicts")
		}
		if err := tx.Commit(); err != nil {
			return TaskGroup{}, false, errors.New("workqueue: task read commit failed")
		}
		return existing, false, nil
	}
	var total, workspaceCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&total); err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task job count unavailable")
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE workspace=?`, spec.Project).Scan(&workspaceCount); err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task project count unavailable")
	}
	if total+spec.WorkerCount > s.config.MaxJobs || workspaceCount+spec.WorkerCount > s.config.MaxJobsPerWorkspace {
		return TaskGroup{}, false, errors.New("workqueue: task queue bound reached")
	}
	taskID, err := randomID("tg_")
	if err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task identity unavailable")
	}
	now := s.clock().UTC()
	if _, err := tx.Exec(`INSERT INTO task_groups(task_id,idempotency_key,project_alias,target_alias,base_commit,goal_hash,pool,profile,worker_count,execution_timeout_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		taskID, spec.IdempotencyKey, spec.Project, spec.Target, spec.BaseCommit, spec.GoalHash, spec.Pool, spec.Profile, spec.WorkerCount, spec.ExecutionTimeoutSeconds, now.UnixNano(), now.UnixNano()); err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task persistence failed")
	}
	for ordinal := 0; ordinal < spec.WorkerCount; ordinal++ {
		jobID, err := randomID("wj_")
		if err != nil {
			return TaskGroup{}, false, errors.New("workqueue: task worker identity unavailable")
		}
		workerKey := fmt.Sprintf("%s:worker:%d", spec.IdempotencyKey, ordinal)
		if !idempotencyPattern.MatchString(workerKey) {
			return TaskGroup{}, false, errors.New("workqueue: task worker idempotency is invalid")
		}
		if _, err := tx.Exec(`INSERT INTO jobs(job_id,idempotency_key,workspace,pool,profile,payload_hash,state,reason,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			jobID, workerKey, spec.Project, spec.Pool, spec.Profile, spec.WorkerGoalHashes[ordinal], StateQueued, ReasonNone, now.UnixNano(), now.UnixNano()); err != nil {
			return TaskGroup{}, false, errors.New("workqueue: task worker persistence failed")
		}
		if _, err := tx.Exec(`INSERT INTO task_workers(task_id,ordinal,job_id,goal_ref) VALUES(?,?,?,?)`, taskID, ordinal, jobID, spec.WorkerGoalRefs[ordinal]); err != nil {
			return TaskGroup{}, false, errors.New("workqueue: task worker binding failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task commit failed")
	}
	task, found, err := s.taskUnlocked(taskID)
	if err != nil || !found {
		return TaskGroup{}, false, errors.New("workqueue: created task unavailable")
	}
	return task, true, nil
}

func (s *Store) LeaseTaskWorker(taskID string, ordinal int, holder string, ttl time.Duration) (TaskWorker, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(taskID) || ordinal < 0 || ordinal >= MaxTaskWorkers || !holderPattern.MatchString(holder) || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return TaskWorker{}, errors.New("workqueue: task worker lease is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker lease transaction failed")
	}
	defer tx.Rollback()
	now := s.clock().UTC()
	if err := recoverExpired(tx, now); err != nil {
		return TaskWorker{}, err
	}
	var jobID string
	if err := tx.QueryRow(`SELECT job_id FROM task_workers WHERE task_id=? AND ordinal=?`, taskID, ordinal).Scan(&jobID); err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker not found")
	}
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found {
		return TaskWorker{}, errors.New("workqueue: task worker job unavailable")
	}
	if job.State == StateLeased && job.LeaseHolder == holder && job.LeaseExpiresAt.After(now) {
		if err := tx.Commit(); err != nil {
			return TaskWorker{}, errors.New("workqueue: task worker lease read failed")
		}
		return taskWorkerFromJob(ordinal, job, "", "", ""), nil
	}
	if job.State != StateQueued || job.CancelRequested {
		return TaskWorker{}, errors.New("workqueue: task worker is not leasable")
	}
	leaseID, err := randomID("wl_")
	if err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker lease identity unavailable")
	}
	expires := now.Add(ttl)
	result, err := tx.Exec(`UPDATE jobs SET state=?,reason='',attempt=attempt+1,fence=fence+1,lease_id=?,lease_holder=?,lease_until=?,updated_at=? WHERE job_id=? AND state=?`,
		StateLeased, leaseID, holder, expires.UnixNano(), now.UnixNano(), jobID, StateQueued)
	if err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker lease failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return TaskWorker{}, errors.New("workqueue: task worker lease lost race")
	}
	job, found, err = jobByID(tx, jobID)
	if err != nil || !found {
		return TaskWorker{}, errors.New("workqueue: leased task worker unavailable")
	}
	if err := tx.Commit(); err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker lease commit failed")
	}
	return taskWorkerFromJob(ordinal, job, "", "", ""), nil
}

func (s *Store) BindTaskWorkerOperation(binding TaskWorkerOperationBinding) (TaskWorker, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(binding.TaskID) || binding.Ordinal < 0 || binding.Ordinal >= MaxTaskWorkers ||
		!jobIDPattern.MatchString(binding.JobID) || !leaseIDPattern.MatchString(binding.LeaseID) || binding.Fence == 0 || !taskOperationPattern.MatchString(binding.OperationID) {
		return TaskWorker{}, errors.New("workqueue: task worker operation binding is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker operation transaction failed")
	}
	defer tx.Rollback()
	var jobID, operationID string
	if err := tx.QueryRow(`SELECT job_id,operation_id FROM task_workers WHERE task_id=? AND ordinal=?`, binding.TaskID, binding.Ordinal).Scan(&jobID, &operationID); err != nil || jobID != binding.JobID {
		return TaskWorker{}, errors.New("workqueue: task worker operation conflicts")
	}
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found || job.State != StateLeased || job.LeaseID != binding.LeaseID || job.Fence != binding.Fence || !job.LeaseExpiresAt.After(s.clock().UTC()) {
		return TaskWorker{}, errors.New("workqueue: stale task worker operation rejected")
	}
	if operationID != "" && operationID != binding.OperationID {
		return TaskWorker{}, errors.New("workqueue: task worker operation conflicts")
	}
	if operationID == "" {
		if _, err := tx.Exec(`UPDATE task_workers SET operation_id=? WHERE task_id=? AND ordinal=? AND operation_id=''`, binding.OperationID, binding.TaskID, binding.Ordinal); err != nil {
			return TaskWorker{}, errors.New("workqueue: task worker operation binding failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker operation commit failed")
	}
	worker := taskWorkerFromJob(binding.Ordinal, job, "", "", "")
	worker.OperationID = binding.OperationID
	return worker, nil
}

func (s *Store) BindTaskWorker(binding TaskWorkerBinding) (TaskWorker, error) {
	if s == nil || s.db == nil || !validTaskWorkerBinding(binding) {
		return TaskWorker{}, errors.New("workqueue: task worker binding is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker binding transaction failed")
	}
	defer tx.Rollback()
	var jobID, worktreeID, workspaceID, runtimeID string
	if err := tx.QueryRow(`SELECT job_id,worktree_id,workspace_id,runtime_id FROM task_workers WHERE task_id=? AND ordinal=?`, binding.TaskID, binding.Ordinal).Scan(&jobID, &worktreeID, &workspaceID, &runtimeID); err != nil || jobID != binding.JobID {
		return TaskWorker{}, errors.New("workqueue: task worker binding conflicts")
	}
	job, found, err := jobByID(tx, jobID)
	if err != nil || !found || job.State != StateLeased || job.LeaseID != binding.LeaseID || job.Fence != binding.Fence || !job.LeaseExpiresAt.After(s.clock().UTC()) {
		return TaskWorker{}, errors.New("workqueue: stale task worker binding rejected")
	}
	if worktreeID != "" && (worktreeID != binding.WorktreeID || workspaceID != binding.WorkspaceID || (runtimeID != "" && runtimeID != binding.RuntimeID)) {
		return TaskWorker{}, errors.New("workqueue: task worker binding conflicts")
	}
	if worktreeID == "" {
		if _, err := tx.Exec(`UPDATE task_workers SET worktree_id=?,workspace_id=?,runtime_id=? WHERE task_id=? AND ordinal=? AND worktree_id=''`, binding.WorktreeID, binding.WorkspaceID, binding.RuntimeID, binding.TaskID, binding.Ordinal); err != nil {
			return TaskWorker{}, errors.New("workqueue: task worker binding failed")
		}
	} else if runtimeID == "" && binding.RuntimeID != "" {
		if _, err := tx.Exec(`UPDATE task_workers SET runtime_id=? WHERE task_id=? AND ordinal=? AND runtime_id=''`, binding.RuntimeID, binding.TaskID, binding.Ordinal); err != nil {
			return TaskWorker{}, errors.New("workqueue: task worker runtime binding failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return TaskWorker{}, errors.New("workqueue: task worker binding commit failed")
	}
	return taskWorkerFromJob(binding.Ordinal, job, binding.WorktreeID, binding.WorkspaceID, binding.RuntimeID), nil
}

func (s *Store) CompleteTaskWorker(taskID string, ordinal int, leaseID string, fence uint64, result Result) (TaskGroup, error) {
	if !taskIDPattern.MatchString(taskID) || ordinal < 0 || ordinal >= MaxTaskWorkers {
		return TaskGroup{}, errors.New("workqueue: task worker completion is invalid")
	}
	task, found, err := s.Task(taskID)
	if err != nil || !found || ordinal >= len(task.Workers) {
		return TaskGroup{}, errors.New("workqueue: task worker not found")
	}
	worker := task.Workers[ordinal]
	if _, err := s.Complete(worker.JobID, leaseID, fence, result); err != nil {
		return TaskGroup{}, err
	}
	updated, found, err := s.Task(taskID)
	if err != nil || !found {
		return TaskGroup{}, errors.New("workqueue: completed task unavailable")
	}
	return updated, nil
}

func (s *Store) CancelTask(taskID string) (TaskGroup, error) {
	task, found, err := s.Task(taskID)
	if err != nil || !found {
		return TaskGroup{}, errors.New("workqueue: task not found")
	}
	for _, worker := range task.Workers {
		if _, err := s.Cancel(worker.JobID); err != nil {
			return TaskGroup{}, err
		}
	}
	updated, found, err := s.Task(taskID)
	if err != nil || !found {
		return TaskGroup{}, errors.New("workqueue: cancelled task unavailable")
	}
	return updated, nil
}

func (s *Store) Task(taskID string) (TaskGroup, bool, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(taskID) {
		return TaskGroup{}, false, errors.New("workqueue: task id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskUnlocked(taskID)
}

// Tasks returns a bounded deterministic view of nonterminal groups for restart
// reconciliation. Retained terminal evidence cannot starve newer runnable groups.
func (s *Store) Tasks(limit int) ([]TaskGroup, error) {
	if s == nil || s.db == nil || limit < 1 || limit > MaxListResults {
		return nil, errors.New("workqueue: task list limit is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT tg.task_id FROM task_groups tg
		WHERE EXISTS(
			SELECT 1 FROM task_workers tw JOIN jobs j ON j.job_id=tw.job_id
			WHERE tw.task_id=tg.task_id AND j.state NOT IN (?,?,?)
		)
		ORDER BY tg.created_at,tg.task_id LIMIT ?`, StateSucceeded, StateFailed, StateCancelled, limit)
	if err != nil {
		return nil, errors.New("workqueue: task list failed")
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.New("workqueue: task list failed")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("workqueue: task list failed")
	}
	if err := rows.Close(); err != nil {
		return nil, errors.New("workqueue: task list failed")
	}
	tasks := make([]TaskGroup, 0, len(ids))
	for _, id := range ids {
		task, found, err := s.taskUnlocked(id)
		if err != nil || !found {
			return nil, errors.New("workqueue: task list contains invalid state")
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Store) taskUnlocked(taskID string) (TaskGroup, bool, error) {
	return taskByID(s.db, taskID)
}

func normalizeTaskSpec(spec TaskSpec) TaskSpec {
	spec.IdempotencyKey = strings.TrimSpace(spec.IdempotencyKey)
	spec.Project = strings.ToLower(strings.TrimSpace(spec.Project))
	spec.Target = strings.ToLower(strings.TrimSpace(spec.Target))
	spec.BaseCommit = strings.ToLower(strings.TrimSpace(spec.BaseCommit))
	spec.GoalHash = strings.ToLower(strings.TrimSpace(spec.GoalHash))
	spec.Pool = strings.ToLower(strings.TrimSpace(spec.Pool))
	spec.Profile = strings.ToLower(strings.TrimSpace(spec.Profile))
	for index := range spec.WorkerGoalHashes {
		spec.WorkerGoalHashes[index] = strings.ToLower(strings.TrimSpace(spec.WorkerGoalHashes[index]))
	}
	for index := range spec.WorkerGoalRefs {
		spec.WorkerGoalRefs[index] = strings.TrimSpace(spec.WorkerGoalRefs[index])
	}
	return spec
}

func validateTaskSpec(spec TaskSpec) error {
	if len(spec.IdempotencyKey) > 118 || !idempotencyPattern.MatchString(spec.IdempotencyKey) || !taskProjectPattern.MatchString(spec.Project) || !taskTargetPattern.MatchString(spec.Target) ||
		!taskCommitPattern.MatchString(spec.BaseCommit) || !payloadHashPattern.MatchString(spec.GoalHash) || !poolPattern.MatchString(spec.Pool) ||
		!profilePattern.MatchString(spec.Profile) || spec.WorkerCount < 1 || spec.WorkerCount > MaxTaskWorkers || spec.ExecutionTimeoutSeconds < 1 || spec.ExecutionTimeoutSeconds > 86400 || len(spec.WorkerGoalHashes) != spec.WorkerCount || len(spec.WorkerGoalRefs) != spec.WorkerCount {
		return errors.New("workqueue: task specification is invalid")
	}
	for index, hash := range spec.WorkerGoalHashes {
		if !payloadHashPattern.MatchString(hash) || !taskGoalRefPattern.MatchString(spec.WorkerGoalRefs[index]) {
			return errors.New("workqueue: task worker goal is invalid")
		}
	}
	return nil
}

func validTaskWorkerBinding(binding TaskWorkerBinding) bool {
	return taskIDPattern.MatchString(binding.TaskID) && binding.Ordinal >= 0 && binding.Ordinal < MaxTaskWorkers && jobIDPattern.MatchString(binding.JobID) &&
		leaseIDPattern.MatchString(binding.LeaseID) && binding.Fence > 0 && taskWorktreePattern.MatchString(binding.WorktreeID) &&
		taskWorkspacePattern.MatchString(binding.WorkspaceID) && (binding.RuntimeID == "" || taskRuntimePattern.MatchString(binding.RuntimeID))
}

func taskIDByIdempotency(scanner interface{ QueryRow(string, ...any) *sql.Row }, key string) (string, bool, error) {
	var id string
	err := scanner.QueryRow(`SELECT task_id FROM task_groups WHERE idempotency_key=?`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.New("workqueue: task idempotency lookup failed")
	}
	return id, true, nil
}

func taskByID(scanner interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}, taskID string) (TaskGroup, bool, error) {
	var task TaskGroup
	var createdAt, updatedAt int64
	err := scanner.QueryRow(`SELECT task_id,idempotency_key,project_alias,target_alias,base_commit,goal_hash,pool,profile,worker_count,execution_timeout_seconds,created_at,updated_at FROM task_groups WHERE task_id=?`, taskID).Scan(
		&task.ID, &task.IdempotencyKey, &task.Project, &task.Target, &task.BaseCommit, &task.GoalHash, &task.Pool, &task.Profile, &task.WorkerCount, &task.ExecutionTimeoutSeconds, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskGroup{}, false, nil
	}
	if err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task read failed")
	}
	task.CreatedAt, task.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
	rows, err := scanner.Query(`SELECT tw.ordinal,j.job_id,j.state,j.reason,j.cancel_requested,j.attempt,j.fence,COALESCE(j.lease_id,''),COALESCE(j.lease_holder,''),COALESCE(j.lease_until,0),tw.operation_id,tw.worktree_id,tw.workspace_id,tw.runtime_id,COALESCE(j.summary,''),j.payload_hash,tw.goal_ref,j.updated_at FROM task_workers tw JOIN jobs j ON j.job_id=tw.job_id WHERE tw.task_id=? ORDER BY tw.ordinal`, taskID)
	if err != nil {
		return TaskGroup{}, false, errors.New("workqueue: task workers unavailable")
	}
	defer rows.Close()
	for rows.Next() {
		var worker TaskWorker
		var leaseUntil, workerUpdatedAt int64
		if err := rows.Scan(&worker.Ordinal, &worker.JobID, &worker.State, &worker.Reason, &worker.CancelRequested, &worker.Attempt, &worker.Fence, &worker.LeaseID, &worker.LeaseHolder, &leaseUntil, &worker.OperationID, &worker.WorktreeID, &worker.WorkspaceID, &worker.RuntimeID, &worker.Summary, &worker.GoalHash, &worker.GoalRef, &workerUpdatedAt); err != nil {
			return TaskGroup{}, false, errors.New("workqueue: task worker read failed")
		}
		worker.UpdatedAt = time.Unix(0, workerUpdatedAt).UTC()
		if worker.UpdatedAt.After(task.UpdatedAt) {
			task.UpdatedAt = worker.UpdatedAt
		}
		if leaseUntil > 0 {
			worker.LeaseExpiresAt = time.Unix(0, leaseUntil).UTC()
		}
		task.Workers = append(task.Workers, worker)
	}
	if err := rows.Err(); err != nil || len(task.Workers) != task.WorkerCount || !validTask(task) {
		return TaskGroup{}, false, errors.New("workqueue: task state is invalid")
	}
	task.State = deriveTaskState(task.Workers)
	return task, true, nil
}

func taskByIDTx(tx *sql.Tx, taskID string) (TaskGroup, bool, error) {
	return taskByID(tx, taskID)
}

func taskMatchesSpec(task TaskGroup, spec TaskSpec) bool {
	if task.IdempotencyKey != spec.IdempotencyKey || task.Project != spec.Project || task.Target != spec.Target || task.BaseCommit != spec.BaseCommit ||
		task.GoalHash != spec.GoalHash || task.Pool != spec.Pool || task.Profile != spec.Profile || task.WorkerCount != spec.WorkerCount || task.ExecutionTimeoutSeconds != spec.ExecutionTimeoutSeconds {
		return false
	}
	for index, worker := range task.Workers {
		if worker.GoalHash != spec.WorkerGoalHashes[index] {
			return false
		}
	}
	return true
}

func validTask(task TaskGroup) bool {
	workerHashes := make([]string, 0, len(task.Workers))
	for _, worker := range task.Workers {
		workerHashes = append(workerHashes, worker.GoalHash)
	}
	workerRefs := make([]string, 0, len(task.Workers))
	for _, worker := range task.Workers {
		workerRefs = append(workerRefs, worker.GoalRef)
	}
	if !taskIDPattern.MatchString(task.ID) || validateTaskSpec(TaskSpec{IdempotencyKey: task.IdempotencyKey, Project: task.Project, Target: task.Target, BaseCommit: task.BaseCommit, GoalHash: task.GoalHash, WorkerGoalHashes: workerHashes, WorkerGoalRefs: workerRefs, Pool: task.Pool, Profile: task.Profile, WorkerCount: task.WorkerCount, ExecutionTimeoutSeconds: task.ExecutionTimeoutSeconds}) != nil ||
		task.CreatedAt.IsZero() || task.UpdatedAt.Before(task.CreatedAt) {
		return false
	}
	for ordinal, worker := range task.Workers {
		if worker.Ordinal != ordinal || !jobIDPattern.MatchString(worker.JobID) || !validState(worker.State) ||
			(worker.LeaseID != "" && (worker.Fence == 0 || !leaseIDPattern.MatchString(worker.LeaseID))) ||
			(worker.State == StateLeased && (worker.Fence == 0 || worker.LeaseID == "" || !holderPattern.MatchString(worker.LeaseHolder))) ||
			(worker.OperationID != "" && !taskOperationPattern.MatchString(worker.OperationID)) ||
			(worker.WorktreeID != "" && !taskWorktreePattern.MatchString(worker.WorktreeID)) || (worker.WorkspaceID != "" && !taskWorkspacePattern.MatchString(worker.WorkspaceID)) ||
			(worker.RuntimeID != "" && !taskRuntimePattern.MatchString(worker.RuntimeID)) || worker.UpdatedAt.IsZero() || worker.UpdatedAt.Before(task.CreatedAt) {
			return false
		}
	}
	return true
}

func validState(state State) bool {
	return state == StateBlocked || state == StateQueued || state == StateLeased || state == StateSucceeded || state == StateFailed || state == StateCancelled
}

func deriveTaskState(workers []TaskWorker) TaskState {
	allSucceeded, allTerminal, anyFailed, anyCancelled, anyLeased, anyStarted, anyCancelRequested := true, true, false, false, false, false, false
	for _, worker := range workers {
		allSucceeded = allSucceeded && worker.State == StateSucceeded
		allTerminal = allTerminal && terminal(worker.State)
		anyFailed = anyFailed || worker.State == StateFailed
		anyCancelled = anyCancelled || worker.State == StateCancelled
		anyLeased = anyLeased || worker.State == StateLeased
		anyStarted = anyStarted || worker.Attempt > 0 || terminal(worker.State)
		anyCancelRequested = anyCancelRequested || worker.CancelRequested
	}
	switch {
	case allSucceeded:
		return TaskCompleted
	case anyFailed:
		return TaskFailed
	case allTerminal && anyCancelled:
		return TaskCancelled
	case anyCancelRequested:
		return TaskCancelling
	case anyLeased || anyStarted:
		return TaskRunning
	default:
		return TaskQueued
	}
}

func taskWorkerFromJob(ordinal int, job Job, worktreeID, workspaceID, runtimeID string) TaskWorker {
	return TaskWorker{
		Ordinal: ordinal, JobID: job.ID, State: job.State, Reason: job.Reason, CancelRequested: job.CancelRequested,
		Attempt: job.Attempt, LeaseID: job.LeaseID, LeaseHolder: job.LeaseHolder, Fence: job.Fence, LeaseExpiresAt: job.LeaseExpiresAt,
		WorktreeID: worktreeID, WorkspaceID: workspaceID, RuntimeID: runtimeID, Summary: job.Summary,
		GoalHash:  job.PayloadHash,
		GoalRef:   "",
		UpdatedAt: job.UpdatedAt,
	}
}

// SortTaskWorkers provides deterministic presentation after callers combine durable results.
func SortTaskWorkers(workers []TaskWorker) {
	sort.Slice(workers, func(i, j int) bool { return workers[i].Ordinal < workers[j].Ordinal })
}
