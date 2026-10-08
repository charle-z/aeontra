package workqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// TaskObjectiveContract declares which operator-owned test profile and source
// criteria evaluate the goal. It is opt-in; a profile result alone remains
// evidence, not acceptance of an undeclared natural-language objective.
type TaskObjectiveContract struct {
	Version                      int  `json:"version"`
	MinimumCommitsAheadPerWorker int  `json:"minimum_commits_ahead_per_worker"`
	MinimumChangedPathsPerWorker int  `json:"minimum_changed_paths_per_worker"`
	RequireClean                 bool `json:"require_clean"`
}

// All criteria are explicit on the wire: omission cannot silently select a
// weaker default. Internal Go callers can still construct a zero-change goal.
func (c *TaskObjectiveContract) UnmarshalJSON(data []byte) error {
	var wire struct {
		Version                      *int  `json:"version"`
		MinimumCommitsAheadPerWorker *int  `json:"minimum_commits_ahead_per_worker"`
		MinimumChangedPathsPerWorker *int  `json:"minimum_changed_paths_per_worker"`
		RequireClean                 *bool `json:"require_clean"`
	}
	if err := decodeTaskObjectiveRecord(string(data), &wire); err != nil {
		return err
	}
	if wire.Version == nil || wire.MinimumCommitsAheadPerWorker == nil || wire.MinimumChangedPathsPerWorker == nil || wire.RequireClean == nil {
		return errors.New("workqueue: every objective criterion must be explicit")
	}
	*c = TaskObjectiveContract{Version: *wire.Version, MinimumCommitsAheadPerWorker: *wire.MinimumCommitsAheadPerWorker, MinimumChangedPathsPerWorker: *wire.MinimumChangedPathsPerWorker, RequireClean: *wire.RequireClean}
	return ValidateTaskObjectiveContract(c)
}

func ValidateTaskObjectiveContract(c *TaskObjectiveContract) error {
	if c == nil {
		return nil
	}
	if c.Version != 1 || c.MinimumCommitsAheadPerWorker < 0 || c.MinimumCommitsAheadPerWorker > 10000 || c.MinimumChangedPathsPerWorker < 0 || c.MinimumChangedPathsPerWorker > 10000 {
		return errors.New("workqueue: objective contract is invalid")
	}
	return nil
}

func SameTaskObjectiveContract(a, b *TaskObjectiveContract) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TaskObjectiveCriteriaSatisfied(c *TaskObjectiveContract, clean bool, ahead, changed int) bool {
	return c != nil && ValidateTaskObjectiveContract(c) == nil && (!c.RequireClean || clean) &&
		ahead >= c.MinimumCommitsAheadPerWorker && ahead <= 10000 && changed >= c.MinimumChangedPathsPerWorker && changed <= 10000
}

// The test receipt already binds the task, worker, workspace, worktree, lease,
// fence, profile and exact source digest. This immutable receipt binds those
// facts to the declared goal contract and the observed Git criteria.
type TaskObjectiveReceipt struct {
	Version           int       `json:"version"`
	TaskID            string    `json:"task_id"`
	Ordinal           int       `json:"ordinal"`
	ContractDigest    string    `json:"contract_digest"`
	TestReceiptDigest string    `json:"test_receipt_digest"`
	HeadCommit        string    `json:"head_commit"`
	Clean             bool      `json:"clean"`
	CommitsAheadBase  int       `json:"commits_ahead_base"`
	ChangedPathCount  int       `json:"changed_path_count"`
	RecordedAt        time.Time `json:"recorded_at"`
}

func TaskObjectiveContractDigest(task TaskGroup, worker TaskWorker) string {
	if task.ObjectiveContract == nil || ValidateTaskObjectiveContract(task.ObjectiveContract) != nil || task.TestAcceptanceContract == nil {
		return ""
	}
	c := task.ObjectiveContract
	canonical := fmt.Sprintf("task-objective-v%d\n%s\n%s\n%s\n%d\n%d\n%t", c.Version, task.GoalHash, worker.GoalHash, TaskTestAcceptanceContractDigest(task.TestAcceptanceContract), c.MinimumCommitsAheadPerWorker, c.MinimumChangedPathsPerWorker, c.RequireClean)
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TaskTestReceiptDigest(receipt TaskTestAcceptanceReceipt) string {
	data, err := json.Marshal(receipt)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validTaskObjectiveReceipt(task TaskGroup, worker TaskWorker, receipt TaskObjectiveReceipt) bool {
	return receipt.Version == 1 && receipt.TaskID == task.ID && receipt.Ordinal == worker.Ordinal && worker.State == StateSucceeded &&
		worker.TestAcceptanceReceipt != nil && validTaskTestAcceptanceReceipt(task, worker, *worker.TestAcceptanceReceipt) &&
		receipt.ContractDigest != "" && receipt.ContractDigest == TaskObjectiveContractDigest(task, worker) &&
		receipt.TestReceiptDigest == TaskTestReceiptDigest(*worker.TestAcceptanceReceipt) && receipt.HeadCommit == worker.TestAcceptanceReceipt.HeadCommit &&
		TaskObjectiveCriteriaSatisfied(task.ObjectiveContract, receipt.Clean, receipt.CommitsAheadBase, receipt.ChangedPathCount) &&
		((receipt.HeadCommit == task.BaseCommit) == (receipt.CommitsAheadBase == 0)) && !receipt.RecordedAt.IsZero() && !receipt.RecordedAt.Before(task.CreatedAt)
}

func decodeTaskObjectiveRecord(record string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(record))
	decoder.DisallowUnknownFields()
	if len(record) > 4096 || decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("workqueue: objective record is corrupt")
	}
	return nil
}

func (s *Store) RecordTaskWorkerObjective(receipt TaskObjectiveReceipt) (TaskObjectiveReceipt, error) {
	if s == nil || s.db == nil || !taskIDPattern.MatchString(receipt.TaskID) || receipt.Ordinal < 0 || receipt.Ordinal >= MaxTaskWorkers {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective transaction failed")
	}
	defer tx.Rollback()
	task, found, err := taskByIDTx(tx, receipt.TaskID)
	if err != nil || !found || receipt.Ordinal >= len(task.Workers) {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective worker unavailable")
	}
	worker := task.Workers[receipt.Ordinal]
	if worker.ObjectiveReceipt != nil {
		original := *worker.ObjectiveReceipt
		receipt.RecordedAt = original.RecordedAt
		if receipt != original {
			return TaskObjectiveReceipt{}, errors.New("workqueue: objective receipt conflicts")
		}
		if err := tx.Commit(); err != nil {
			return TaskObjectiveReceipt{}, errors.New("workqueue: objective replay failed")
		}
		return original, nil
	}
	receipt.RecordedAt = s.clock().UTC()
	if worker.WorktreeCleaned || !validTaskObjectiveReceipt(task, worker, receipt) {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective evidence is stale or invalid")
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective encoding failed")
	}
	result, err := tx.Exec(`UPDATE task_workers SET objective_receipt=? WHERE task_id=? AND ordinal=? AND objective_receipt=''`, string(encoded), receipt.TaskID, receipt.Ordinal)
	if err != nil {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective persistence failed")
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective receipt conflicts")
	}
	if _, err := tx.Exec(`UPDATE task_groups SET updated_at=? WHERE task_id=?`, receipt.RecordedAt.UnixNano(), receipt.TaskID); err != nil {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective timestamp persistence failed")
	}
	if err := tx.Commit(); err != nil {
		return TaskObjectiveReceipt{}, errors.New("workqueue: objective commit failed")
	}
	return receipt, nil
}
