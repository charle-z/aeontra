package edge

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	MinLeaseTTL = 15 * time.Second
	MaxLeaseTTL = 10 * time.Minute
)

var ErrNoTaskAvailable = errors.New("no edge task available")

type TaskState string
type Outcome string
type NetworkPolicy string

const (
	TaskQueued    TaskState = "queued"
	TaskLeased    TaskState = "leased"
	TaskSucceeded TaskState = "succeeded"
	TaskFailed    TaskState = "failed"
	TaskCancelled TaskState = "cancelled"

	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCancelled Outcome = "cancelled"

	NetworkNone     NetworkPolicy = "none"
	NetworkRegistry NetworkPolicy = "registry"
)

type Objective struct {
	Summary    string   `json:"summary"`
	Acceptance []string `json:"acceptance"`
}

type Restrictions struct {
	Workspace          string        `json:"workspace"`
	NetworkPolicy      NetworkPolicy `json:"network_policy"`
	MaxDurationSeconds int           `json:"max_duration_seconds"`
	MaxOutputBytes     int64         `json:"max_output_bytes"`
}

type TaskSpec struct {
	IdempotencyKey string       `json:"idempotency_key"`
	Workcell       string       `json:"workcell"`
	Objective      Objective    `json:"objective"`
	Restrictions   Restrictions `json:"restrictions"`
}

type Task struct {
	ID             string       `json:"task_id"`
	IdempotencyKey string       `json:"idempotency_key"`
	Workcell       string       `json:"workcell"`
	Objective      Objective    `json:"objective"`
	Restrictions   Restrictions `json:"restrictions"`
	State          TaskState    `json:"state"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	Outcome        Outcome      `json:"outcome,omitempty"`
	ResultSummary  string       `json:"result_summary,omitempty"`
	ResultRef      string       `json:"result_ref,omitempty"`
}

type Lease struct {
	Task           Task      `json:"task"`
	LeaseID        string    `json:"lease_id"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	Attempt        int       `json:"attempt"`
}

type HeartbeatStatus struct {
	LeaseExpiresAt  time.Time `json:"lease_expires_at"`
	CancelRequested bool      `json:"cancel_requested"`
}

type TaskResult struct {
	Outcome   Outcome `json:"outcome"`
	Summary   string  `json:"summary"`
	ResultRef string  `json:"result_ref,omitempty"`
}

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var workspacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var holderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,95}$`)
var taskIDPattern = regexp.MustCompile(`^et_[a-f0-9]{32}$`)
var leaseIDPattern = regexp.MustCompile(`^el_[a-f0-9]{32}$`)
var resultRefPattern = regexp.MustCompile(`^rs_[a-f0-9]{32,64}$`)

func (s *Store) CreateTask(deviceID string, spec TaskSpec) (Task, bool, error) {
	if !idPattern.MatchString(deviceID) || validateTaskSpec(spec) != nil {
		return Task{}, false, errors.New("edge task is invalid")
	}
	objectiveJSON, _ := json.Marshal(spec.Objective)
	restrictionsJSON, _ := json.Marshal(spec.Restrictions)
	s.mu.Lock()
	defer s.mu.Unlock()

	var state State
	if err := s.db.QueryRow(`SELECT state FROM devices WHERE device_id=?`, deviceID).Scan(&state); err != nil || state != StateActive {
		return Task{}, false, errors.New("active edge device not found")
	}
	existing, err := s.taskByIdempotency(deviceID, spec.IdempotencyKey)
	if err == nil {
		if existing.Workcell != spec.Workcell || !objectEqual(existing.Objective, spec.Objective) || !objectEqual(existing.Restrictions, spec.Restrictions) {
			return Task{}, false, errors.New("idempotency key conflicts with existing task")
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, errors.New("edge task persistence failed")
	}
	id, err := randomOpaque("et_", 16)
	if err != nil {
		return Task{}, false, errors.New("edge task generation failed")
	}
	now := s.now().UTC()
	_, err = s.db.Exec(`INSERT INTO edge_tasks(task_id,device_id,idempotency_key,workcell,objective_json,restrictions_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, deviceID, spec.IdempotencyKey, spec.Workcell, objectiveJSON, restrictionsJSON, TaskQueued, now.Unix(), now.Unix())
	if err != nil {
		return Task{}, false, errors.New("edge task persistence failed")
	}
	return Task{ID: id, IdempotencyKey: spec.IdempotencyKey, Workcell: spec.Workcell, Objective: spec.Objective, Restrictions: spec.Restrictions, State: TaskQueued, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (s *Store) LeaseNext(deviceID, workcell, holder string, ttl time.Duration) (Lease, error) {
	if !idPattern.MatchString(deviceID) || workcell != "development" || !holderPattern.MatchString(holder) || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return Lease{}, errors.New("lease request is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE edge_tasks SET state=?,lease_id=NULL,lease_holder=NULL,lease_until=NULL,updated_at=? WHERE device_id=? AND state=? AND lease_until<=?`, TaskQueued, now.Unix(), deviceID, TaskLeased, now.Unix()); err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	if lease, err := leaseForHolder(tx, deviceID, workcell, holder, now); err == nil {
		return lease, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Lease{}, errors.New("lease unavailable")
	}
	var taskID string
	if err := tx.QueryRow(`SELECT task_id FROM edge_tasks WHERE device_id=? AND workcell=? AND state=? AND cancel_requested=0 ORDER BY created_at,task_id LIMIT 1`, deviceID, workcell, TaskQueued).Scan(&taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Lease{}, ErrNoTaskAvailable
		}
		return Lease{}, errors.New("lease unavailable")
	}
	leaseID, err := randomOpaque("el_", 16)
	if err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	expires := now.Add(ttl)
	if _, err := tx.Exec(`UPDATE edge_tasks SET state=?,lease_id=?,lease_holder=?,lease_until=?,attempt_count=attempt_count+1,updated_at=? WHERE task_id=? AND state=?`, TaskLeased, leaseID, holder, expires.Unix(), now.Unix(), taskID, TaskQueued); err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	lease, err := leaseByID(tx, deviceID, taskID, leaseID)
	if err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	if err := tx.Commit(); err != nil {
		return Lease{}, errors.New("lease unavailable")
	}
	return lease, nil
}

func (s *Store) Heartbeat(deviceID, taskID, leaseID string, ttl time.Duration) (HeartbeatStatus, error) {
	if !validLeaseIdentity(deviceID, taskID, leaseID) || ttl < MinLeaseTTL || ttl > MaxLeaseTTL {
		return HeartbeatStatus{}, errors.New("heartbeat is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	expires := now.Add(ttl)
	result, err := s.db.Exec(`UPDATE edge_tasks SET lease_until=?,updated_at=? WHERE device_id=? AND task_id=? AND lease_id=? AND state=? AND lease_until>?`, expires.Unix(), now.Unix(), deviceID, taskID, leaseID, TaskLeased, now.Unix())
	if err != nil {
		return HeartbeatStatus{}, errors.New("heartbeat unavailable")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return HeartbeatStatus{}, errors.New("active lease not found")
	}
	var cancelled bool
	if err := s.db.QueryRow(`SELECT cancel_requested FROM edge_tasks WHERE task_id=?`, taskID).Scan(&cancelled); err != nil {
		return HeartbeatStatus{}, errors.New("heartbeat unavailable")
	}
	return HeartbeatStatus{LeaseExpiresAt: expires, CancelRequested: cancelled}, nil
}

func (s *Store) Complete(deviceID, taskID, leaseID string, result TaskResult) (Task, error) {
	result.Summary, _ = policy.Redact(result.Summary)
	if !validLeaseIdentity(deviceID, taskID, leaseID) || validateTaskResult(result) != nil {
		return Task{}, errors.New("task completion is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.taskByID(taskID)
	if err != nil {
		return Task{}, errors.New("edge task not found")
	}
	if existing.State == TaskSucceeded || existing.State == TaskFailed || existing.State == TaskCancelled {
		var storedLease string
		if err := s.db.QueryRow(`SELECT lease_id FROM edge_tasks WHERE task_id=?`, taskID).Scan(&storedLease); err == nil && storedLease == leaseID && existing.Outcome == result.Outcome && existing.ResultSummary == result.Summary && existing.ResultRef == result.ResultRef {
			return existing, nil
		}
		return Task{}, errors.New("task completion conflicts with terminal result")
	}
	now := s.now().UTC()
	state := stateForOutcome(result.Outcome)
	update, err := s.db.Exec(`UPDATE edge_tasks SET state=?,outcome=?,result_summary=?,result_ref=?,updated_at=? WHERE device_id=? AND task_id=? AND lease_id=? AND state=? AND lease_until>?`, state, result.Outcome, result.Summary, nullableString(result.ResultRef), now.Unix(), deviceID, taskID, leaseID, TaskLeased, now.Unix())
	if err != nil {
		return Task{}, errors.New("task completion unavailable")
	}
	rows, _ := update.RowsAffected()
	if rows != 1 {
		return Task{}, errors.New("active lease not found")
	}
	return s.taskByID(taskID)
}

func (s *Store) CancelTask(taskID string) error {
	if !taskIDPattern.MatchString(taskID) {
		return errors.New("task id is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC().Unix()
	result, err := s.db.Exec(`UPDATE edge_tasks SET cancel_requested=1,state=CASE WHEN state=? THEN ? ELSE state END,outcome=CASE WHEN state=? THEN ? ELSE outcome END,updated_at=? WHERE task_id=? AND state IN (?,?)`, TaskQueued, TaskCancelled, TaskQueued, OutcomeCancelled, now, taskID, TaskQueued, TaskLeased)
	if err != nil {
		return errors.New("task cancellation failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("cancellable task not found")
	}
	return nil
}

func (s *Store) TaskStatus(taskID string) (Task, error) {
	if !taskIDPattern.MatchString(taskID) {
		return Task{}, errors.New("task id is invalid")
	}
	task, err := s.taskByID(taskID)
	if err != nil {
		return Task{}, errors.New("edge task not found")
	}
	return task, nil
}

func validateTaskSpec(spec TaskSpec) error {
	if !idempotencyPattern.MatchString(spec.IdempotencyKey) || spec.Workcell != "development" || len(spec.Objective.Summary) == 0 || len(spec.Objective.Summary) > 2048 || len(spec.Objective.Acceptance) > 8 {
		return errors.New("invalid task specification")
	}
	if redacted, changed := policy.Redact(spec.Objective.Summary); changed || redacted != spec.Objective.Summary {
		return errors.New("task objective contains secret material")
	}
	for _, item := range spec.Objective.Acceptance {
		if len(item) == 0 || len(item) > 256 {
			return errors.New("invalid acceptance criterion")
		}
		if redacted, changed := policy.Redact(item); changed || redacted != item {
			return errors.New("acceptance criterion contains secret material")
		}
	}
	r := spec.Restrictions
	if !workspacePattern.MatchString(r.Workspace) || (r.NetworkPolicy != NetworkNone && r.NetworkPolicy != NetworkRegistry) || r.MaxDurationSeconds < 30 || r.MaxDurationSeconds > 3600 || r.MaxOutputBytes < 1024 || r.MaxOutputBytes > 1<<20 {
		return errors.New("invalid task restrictions")
	}
	return nil
}

func validateTaskResult(result TaskResult) error {
	if stateForOutcome(result.Outcome) == "" || len(result.Summary) == 0 || len(result.Summary) > 2048 || (result.ResultRef != "" && !resultRefPattern.MatchString(result.ResultRef)) {
		return errors.New("invalid task result")
	}
	return nil
}

func stateForOutcome(outcome Outcome) TaskState {
	switch outcome {
	case OutcomeSucceeded:
		return TaskSucceeded
	case OutcomeFailed:
		return TaskFailed
	case OutcomeCancelled:
		return TaskCancelled
	default:
		return ""
	}
}

func validLeaseIdentity(deviceID, taskID, leaseID string) bool {
	return idPattern.MatchString(deviceID) && taskIDPattern.MatchString(taskID) && leaseIDPattern.MatchString(leaseID)
}

func objectEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) taskByIdempotency(deviceID, key string) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT task_id,idempotency_key,workcell,objective_json,restrictions_json,state,created_at,updated_at,outcome,result_summary,result_ref FROM edge_tasks WHERE device_id=? AND idempotency_key=?`, deviceID, key))
}

func (s *Store) taskByID(taskID string) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT task_id,idempotency_key,workcell,objective_json,restrictions_json,state,created_at,updated_at,outcome,result_summary,result_ref FROM edge_tasks WHERE task_id=?`, taskID))
}

type rowScanner interface{ Scan(...any) error }

func scanTask(row rowScanner) (Task, error) {
	var task Task
	var objectiveJSON, restrictionsJSON []byte
	var created, updated int64
	var outcome, summary, resultRef sql.NullString
	err := row.Scan(&task.ID, &task.IdempotencyKey, &task.Workcell, &objectiveJSON, &restrictionsJSON, &task.State, &created, &updated, &outcome, &summary, &resultRef)
	if err != nil {
		return Task{}, err
	}
	if err := json.Unmarshal(objectiveJSON, &task.Objective); err != nil {
		return Task{}, fmt.Errorf("invalid stored objective: %w", err)
	}
	if err := json.Unmarshal(restrictionsJSON, &task.Restrictions); err != nil {
		return Task{}, fmt.Errorf("invalid stored restrictions: %w", err)
	}
	task.CreatedAt = time.Unix(created, 0).UTC()
	task.UpdatedAt = time.Unix(updated, 0).UTC()
	task.Outcome = Outcome(outcome.String)
	task.ResultSummary = summary.String
	task.ResultRef = resultRef.String
	return task, nil
}

func leaseForHolder(tx *sql.Tx, deviceID, workcell, holder string, now time.Time) (Lease, error) {
	row := tx.QueryRow(`SELECT task_id,lease_id,lease_until,attempt_count FROM edge_tasks WHERE device_id=? AND workcell=? AND lease_holder=? AND state=? AND lease_until>? ORDER BY created_at LIMIT 1`, deviceID, workcell, holder, TaskLeased, now.Unix())
	var taskID, leaseID string
	var expires int64
	var attempt int
	if err := row.Scan(&taskID, &leaseID, &expires, &attempt); err != nil {
		return Lease{}, err
	}
	task, err := scanTask(tx.QueryRow(`SELECT task_id,idempotency_key,workcell,objective_json,restrictions_json,state,created_at,updated_at,outcome,result_summary,result_ref FROM edge_tasks WHERE task_id=?`, taskID))
	return Lease{Task: task, LeaseID: leaseID, LeaseExpiresAt: time.Unix(expires, 0).UTC(), Attempt: attempt}, err
}

func leaseByID(tx *sql.Tx, deviceID, taskID, leaseID string) (Lease, error) {
	row := tx.QueryRow(`SELECT lease_until,attempt_count FROM edge_tasks WHERE device_id=? AND task_id=? AND lease_id=?`, deviceID, taskID, leaseID)
	var expires int64
	var attempt int
	if err := row.Scan(&expires, &attempt); err != nil {
		return Lease{}, err
	}
	task, err := scanTask(tx.QueryRow(`SELECT task_id,idempotency_key,workcell,objective_json,restrictions_json,state,created_at,updated_at,outcome,result_summary,result_ref FROM edge_tasks WHERE task_id=?`, taskID))
	return Lease{Task: task, LeaseID: leaseID, LeaseExpiresAt: time.Unix(expires, 0).UTC(), Attempt: attempt}, err
}
