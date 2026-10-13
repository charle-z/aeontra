package workqueue

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

// TaskIntegrationContract selects immutable committed inputs for one responsible
// reviewer/integrator. It grants no publication or source-worktree write authority.
type TaskIntegrationContract struct {
	Version            int                     `json:"version"`
	ExpectedBaseCommit string                  `json:"expected_base_commit"`
	SourceTaskID       string                  `json:"source_task_id"`
	Workers            []TaskIntegrationWorker `json:"workers"`
}

type TaskIntegrationWorker struct {
	Ordinal    int    `json:"ordinal"`
	HeadCommit string `json:"head_commit"`
}

func (w *TaskIntegrationWorker) UnmarshalJSON(data []byte) error {
	var wire struct {
		Ordinal    *int    `json:"ordinal"`
		HeadCommit *string `json:"head_commit"`
	}
	if err := decodeTaskObjectiveRecord(string(data), &wire); err != nil {
		return err
	}
	if wire.Ordinal == nil || wire.HeadCommit == nil {
		return errors.New("workqueue: every integration source selector must be explicit")
	}
	*w = TaskIntegrationWorker{Ordinal: *wire.Ordinal, HeadCommit: *wire.HeadCommit}
	return nil
}

// These bindings are captured by the server, never supplied as caller authority.
type TaskIntegrationSourcePin struct {
	TaskID                 string `json:"task_id"`
	Ordinal                int    `json:"ordinal"`
	JobID                  string `json:"job_id"`
	RuntimeID              string `json:"runtime_id"`
	WorktreeID             string `json:"worktree_id"`
	WorkspaceID            string `json:"workspace_id"`
	BaseCommit             string `json:"base_commit"`
	HeadCommit             string `json:"head_commit"`
	Branch                 string `json:"branch"`
	LeaseID                string `json:"lease_id"`
	Fence                  uint64 `json:"fence"`
	GitReceiptDigest       string `json:"git_receipt_digest,omitempty"`
	TestReceiptDigest      string `json:"test_receipt_digest,omitempty"`
	ObjectiveReceiptDigest string `json:"objective_receipt_digest,omitempty"`
}

type TaskIntegrationReceipt struct {
	Version                int       `json:"version"`
	TaskID                 string    `json:"task_id"`
	ContractDigest         string    `json:"contract_digest"`
	SourcesDigest          string    `json:"sources_digest"`
	ObjectiveReceiptDigest string    `json:"objective_receipt_digest"`
	HeadCommit             string    `json:"head_commit"`
	RecordedAt             time.Time `json:"recorded_at"`
}

func ValidateTaskIntegrationContract(c *TaskIntegrationContract) error {
	if c == nil {
		return nil
	}
	if c.Version != 1 || !taskCommitPattern.MatchString(c.ExpectedBaseCommit) || !taskIDPattern.MatchString(c.SourceTaskID) || len(c.Workers) < 2 || len(c.Workers) > MaxTaskWorkers {
		return errors.New("workqueue: integration contract is invalid")
	}
	seen := make(map[int]bool)
	for _, worker := range c.Workers {
		if worker.Ordinal < 0 || worker.Ordinal >= MaxTaskWorkers || seen[worker.Ordinal] || !taskCommitPattern.MatchString(worker.HeadCommit) {
			return errors.New("workqueue: integration source selection is invalid")
		}
		seen[worker.Ordinal] = true
	}
	return nil
}

func SameTaskIntegrationContract(a, b *TaskIntegrationContract) bool { return reflect.DeepEqual(a, b) }

func TaskIntegrationDigest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("task-integration-v1\n"), data...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TaskIntegrationPin(source TaskGroup, ordinal int, head string) TaskIntegrationSourcePin {
	w := source.Workers[ordinal]
	p := TaskIntegrationSourcePin{TaskID: source.ID, Ordinal: ordinal, JobID: w.JobID, RuntimeID: w.RuntimeID, WorktreeID: w.WorktreeID, WorkspaceID: w.WorkspaceID,
		BaseCommit: source.BaseCommit, HeadCommit: head, Branch: "codex/worktree-" + strings.TrimPrefix(w.WorktreeID, "wt_"), LeaseID: w.LeaseID, Fence: w.Fence}
	if w.AcceptanceReceipt != nil {
		p.GitReceiptDigest = TaskIntegrationDigest(*w.AcceptanceReceipt)
	}
	if w.TestAcceptanceReceipt != nil {
		p.TestReceiptDigest = TaskIntegrationDigest(*w.TestAcceptanceReceipt)
	}
	if w.ObjectiveReceipt != nil {
		p.ObjectiveReceiptDigest = TaskIntegrationDigest(*w.ObjectiveReceipt)
	}
	return p
}

func validTaskIntegrationSpec(spec TaskSpec) bool {
	if spec.IntegrationContract == nil {
		return len(spec.IntegrationPins) == 0
	}
	c := spec.IntegrationContract
	if ValidateTaskIntegrationContract(c) != nil || spec.WorkerCount != 1 || spec.BaseCommit != c.ExpectedBaseCommit || spec.ObjectiveContract == nil || !spec.ObjectiveContract.RequireClean || spec.TestAcceptanceContract == nil || len(spec.IntegrationPins) != len(c.Workers) {
		return false
	}
	for i, p := range spec.IntegrationPins {
		selected := c.Workers[i]
		if p.TaskID != c.SourceTaskID || p.Ordinal != selected.Ordinal || p.HeadCommit != selected.HeadCommit || p.BaseCommit != c.ExpectedBaseCommit ||
			!jobIDPattern.MatchString(p.JobID) || !taskRuntimePattern.MatchString(p.RuntimeID) || !taskWorktreePattern.MatchString(p.WorktreeID) || !taskWorkspacePattern.MatchString(p.WorkspaceID) ||
			!leaseIDPattern.MatchString(p.LeaseID) || p.Fence == 0 || p.Branch != "codex/worktree-"+p.WorktreeID[3:] {
			return false
		}
		for _, digest := range []string{p.GitReceiptDigest, p.TestReceiptDigest, p.ObjectiveReceiptDigest} {
			if digest != "" && !payloadHashPattern.MatchString(digest) {
				return false
			}
		}
	}
	return true
}

func ValidateTaskIntegrationSourceBindings(task, source TaskGroup) error {
	if task.IntegrationContract == nil {
		return nil
	}
	if source.IntegrationContract != nil || source.ID != task.IntegrationContract.SourceTaskID || source.State != TaskCompleted || source.Project != task.Project || source.Target != task.Target || source.BaseCommit != task.BaseCommit {
		return errors.New("workqueue: integration source task changed")
	}
	for _, p := range task.IntegrationPins {
		if p.Ordinal < 0 || p.Ordinal >= len(source.Workers) {
			return errors.New("workqueue: integration source worker unavailable")
		}
		w := source.Workers[p.Ordinal]
		if w.State != StateSucceeded || w.WorktreeCleaned || w.WorktreeID == "" || w.RuntimeID == "" || !reflect.DeepEqual(p, TaskIntegrationPin(source, p.Ordinal, p.HeadCommit)) {
			return errors.New("workqueue: integration source identity or receipt changed")
		}
	}
	return nil
}

// Called in the creation transaction, so durable cleanup markers cannot race pins.
func validateTaskIntegrationSourcesTx(tx interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}, task TaskGroup) error {
	if task.IntegrationContract == nil {
		return nil
	}
	source, found, err := taskByID(tx, task.IntegrationContract.SourceTaskID)
	if err != nil || !found {
		return errors.New("workqueue: integration source task unavailable")
	}
	return ValidateTaskIntegrationSourceBindings(task, source)
}

func validTaskIntegrationReceipt(task TaskGroup, receipt TaskIntegrationReceipt) bool {
	if task.IntegrationContract == nil || len(task.Workers) != 1 || task.Workers[0].ObjectiveReceipt == nil {
		return false
	}
	objective := *task.Workers[0].ObjectiveReceipt
	return receipt.Version == 1 && receipt.TaskID == task.ID && receipt.ContractDigest == TaskIntegrationDigest(task.IntegrationContract) && receipt.SourcesDigest == TaskIntegrationDigest(task.IntegrationPins) &&
		receipt.ObjectiveReceiptDigest == TaskIntegrationDigest(objective) && receipt.HeadCommit == objective.HeadCommit && objective.Clean && !receipt.RecordedAt.IsZero() && !receipt.RecordedAt.Before(task.CreatedAt)
}

func (s *Store) RecordTaskIntegrationReceipt(receipt TaskIntegrationReceipt) (TaskIntegrationReceipt, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(receipt.TaskID) {
		return TaskIntegrationReceipt{}, errors.New("workqueue: integration receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskIntegrationReceipt{}, errors.New("workqueue: integration receipt transaction failed")
	}
	defer tx.Rollback()
	task, found, err := taskByIDTx(tx, receipt.TaskID)
	if err != nil || !found {
		return TaskIntegrationReceipt{}, errors.New("workqueue: integration task unavailable")
	}
	if task.IntegrationReceipt != nil {
		receipt.RecordedAt = task.IntegrationReceipt.RecordedAt
		if receipt != *task.IntegrationReceipt {
			return TaskIntegrationReceipt{}, errors.New("workqueue: integration receipt conflicts")
		}
		return receipt, tx.Commit()
	}
	receipt.RecordedAt = s.clock().UTC()
	if !validTaskIntegrationReceipt(task, receipt) || validateTaskIntegrationSourcesTx(tx, task) != nil {
		return TaskIntegrationReceipt{}, errors.New("workqueue: integration evidence is stale or invalid")
	}
	encoded, _ := json.Marshal(receipt)
	if _, err = tx.Exec(`UPDATE task_groups SET integration_receipt=?,updated_at=? WHERE task_id=? AND integration_receipt=''`, string(encoded), receipt.RecordedAt.UnixNano(), task.ID); err != nil {
		return TaskIntegrationReceipt{}, errors.New("workqueue: integration receipt persistence failed")
	}
	return receipt, tx.Commit()
}

// Sources remain required through candidate testing and verified cleanup. Failed
// or cancelled integrations release only the requirement, never the source data.
func (s *Store) TaskRequiredByIntegration(sourceTaskID string) (bool, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(sourceTaskID) {
		return false, errors.New("workqueue: integration source identity is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return taskRequiredByIntegration(s.db, sourceTaskID)
}

func taskRequiredByIntegration(scanner interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}, sourceTaskID string) (bool, error) {
	var id string
	err := scanner.QueryRow(`SELECT tg.task_id FROM task_groups tg JOIN task_workers tw ON tw.task_id=tg.task_id JOIN jobs j ON j.job_id=tw.job_id WHERE tg.integration_source_task=? AND tg.integration_source_task<>'' AND j.state NOT IN (?,?) AND tw.worktree_cleaned=0 LIMIT 1`, sourceTaskID, StateFailed, StateCancelled).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("workqueue: integration pins unavailable")
	}
	task, found, err := taskByID(scanner, id)
	if err != nil || !found || task.IntegrationContract == nil || task.IntegrationContract.SourceTaskID != sourceTaskID {
		return false, errors.New("workqueue: integration task unavailable")
	}
	return true, nil
}
