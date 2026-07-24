package edgeclient

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

const journalSchemaVersion = 2

var journalResultIDPattern = regexp.MustCompile(`^jr_[a-f0-9]{64}$`)
var journalLeasePattern = regexp.MustCompile(`^el_[a-f0-9]{32}$`)

func migrateJournalV2(db *sql.DB) error {
	if db == nil {
		return errors.New("journal database is unavailable")
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version > journalSchemaVersion {
		return errors.New("journal schema is unsupported")
	}
	columns, err := journalColumns(db)
	if err != nil {
		return err
	}
	for name, statement := range map[string]string{
		"attempt":      `ALTER TABLE executions ADD COLUMN attempt INTEGER NOT NULL DEFAULT 1`,
		"result_id":    `ALTER TABLE executions ADD COLUMN result_id TEXT`,
		"lease_id":     `ALTER TABLE executions ADD COLUMN lease_id TEXT`,
		"delivered_at": `ALTER TABLE executions ADD COLUMN delivered_at INTEGER`,
		"updated_at":   `ALTER TABLE executions ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0`,
	} {
		if columns[name] {
			continue
		}
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`UPDATE executions SET updated_at=CASE WHEN completed_at IS NOT NULL THEN completed_at ELSE started_at END WHERE updated_at=0`); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT idempotency_key,task_id,outcome,summary,result_ref FROM executions WHERE state=? AND (result_id IS NULL OR result_id='')`, JournalCompleted)
	if err != nil {
		return err
	}
	type legacyCompletion struct {
		key, taskID string
		result      edge.TaskResult
	}
	legacy := make([]legacyCompletion, 0)
	for rows.Next() {
		var item legacyCompletion
		var outcome, summary, resultRef sql.NullString
		if err := rows.Scan(&item.key, &item.taskID, &outcome, &summary, &resultRef); err != nil {
			_ = rows.Close()
			return err
		}
		item.result = edge.TaskResult{Outcome: edge.Outcome(outcome.String), Summary: summary.String, ResultRef: resultRef.String}
		if !validJournalResult(item.result) {
			_ = rows.Close()
			return errors.New("legacy journal completion is invalid")
		}
		legacy = append(legacy, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range legacy {
		if _, err := db.Exec(`UPDATE executions SET result_id=? WHERE idempotency_key=? AND task_id=? AND state=? AND (result_id IS NULL OR result_id='')`, journalResultID(item.key, item.taskID, item.result), item.key, item.taskID, JournalCompleted); err != nil {
			return err
		}
	}
	_, err = db.Exec(`PRAGMA user_version=2`)
	return err
}

func journalColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(executions)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (j *Journal) BeginAttempt(key, taskID string, attempt int) (JournalEntry, error) {
	return j.beginLease(key, taskID, attempt, "")
}

func (j *Journal) BeginLease(key, taskID string, attempt int, leaseID string) (JournalEntry, error) {
	if !journalLeasePattern.MatchString(leaseID) {
		return JournalEntry{}, errors.New("journal lease identity is invalid")
	}
	return j.beginLease(key, taskID, attempt, leaseID)
}

func (j *Journal) beginLease(key, taskID string, attempt int, leaseID string) (JournalEntry, error) {
	if j == nil || j.db == nil || !journalKeyPattern.MatchString(key) || !journalTaskPattern.MatchString(taskID) || attempt < 1 || attempt > 1_000_000 || (leaseID != "" && !journalLeasePattern.MatchString(leaseID)) {
		return JournalEntry{}, errors.New("journal identity is invalid")
	}
	tx, err := j.db.Begin()
	if err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	defer tx.Rollback()
	entry, storedTask, found, err := queryJournalEntry(tx.QueryRow(journalEntrySelect+` WHERE idempotency_key=?`, key))
	if err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	if found {
		if storedTask != taskID {
			return JournalEntry{}, errors.New("journal idempotency conflict")
		}
		if leaseID != "" && entry.LeaseID != leaseID {
			if _, err := tx.Exec(`UPDATE executions SET lease_id=?,updated_at=? WHERE idempotency_key=? AND task_id=?`, leaseID, j.clock().UTC().Unix(), key, taskID); err != nil {
				return JournalEntry{}, errors.New("edge journal unavailable")
			}
			entry.LeaseID = leaseID
		}
		if err := tx.Commit(); err != nil {
			return JournalEntry{}, errors.New("edge journal unavailable")
		}
		return entry, nil
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM executions`).Scan(&count); err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	if count >= j.maximumEntries() {
		return JournalEntry{}, errors.New("edge journal capacity exceeded")
	}
	now := j.clock().UTC()
	if _, err := tx.Exec(`INSERT INTO executions(idempotency_key,task_id,state,started_at,attempt,lease_id,updated_at) VALUES(?,?,?,?,?,?,?)`, key, taskID, JournalStarted, now.Unix(), attempt, nullableJournalString(leaseID), now.Unix()); err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	if err := tx.Commit(); err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	return JournalEntry{Key: key, TaskID: taskID, LeaseID: leaseID, State: JournalStarted, New: true, Attempt: attempt, StartedAt: now}, nil
}

func (j *Journal) FinishEntry(key, taskID string, result edge.TaskResult) (JournalEntry, error) {
	if j == nil || j.db == nil || !journalKeyPattern.MatchString(key) || !journalTaskPattern.MatchString(taskID) || !validJournalResult(result) {
		return JournalEntry{}, errors.New("journal completion is invalid")
	}
	existing, err := j.BeginAttempt(key, taskID, 1)
	if err != nil {
		return JournalEntry{}, err
	}
	if existing.State == JournalCompleted {
		if existing.Result == result {
			return existing, nil
		}
		return JournalEntry{}, errors.New("journal completion conflicts")
	}
	resultID := journalResultID(key, taskID, result)
	now := j.clock().UTC()
	update, err := j.db.Exec(`UPDATE executions SET state=?,outcome=?,summary=?,result_ref=?,completed_at=?,result_id=?,updated_at=? WHERE idempotency_key=? AND task_id=? AND state=?`, JournalCompleted, result.Outcome, result.Summary, nullableJournalString(result.ResultRef), now.Unix(), resultID, now.Unix(), key, taskID, JournalStarted)
	if err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	rows, _ := update.RowsAffected()
	if rows != 1 {
		return JournalEntry{}, errors.New("journal completion failed")
	}
	entry, storedTask, found, err := queryJournalEntry(j.db.QueryRow(journalEntrySelect+` WHERE idempotency_key=?`, key))
	if err != nil || !found || storedTask != taskID {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	return entry, nil
}

func (j *Journal) MarkDelivered(key, taskID, resultID string) error {
	if j == nil || j.db == nil || !journalKeyPattern.MatchString(key) || !journalTaskPattern.MatchString(taskID) || !journalResultIDPattern.MatchString(resultID) {
		return errors.New("journal delivery identity is invalid")
	}
	now := j.clock().UTC().Unix()
	result, err := j.db.Exec(`UPDATE executions SET delivered_at=COALESCE(delivered_at,?),updated_at=? WHERE idempotency_key=? AND task_id=? AND state=? AND result_id=?`, now, now, key, taskID, JournalCompleted, resultID)
	if err != nil {
		return errors.New("edge journal unavailable")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("journal delivery identity conflicts")
	}
	return nil
}

func (j *Journal) CleanupDelivered(retention time.Duration) (int64, error) {
	if j == nil || j.db == nil || retention <= 0 || retention > 365*24*time.Hour {
		return 0, errors.New("journal retention is invalid")
	}
	cutoff := j.clock().UTC().Add(-retention).Unix()
	result, err := j.db.Exec(`DELETE FROM executions WHERE delivered_at IS NOT NULL AND delivered_at<=?`, cutoff)
	if err != nil {
		return 0, errors.New("edge journal unavailable")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, errors.New("edge journal unavailable")
	}
	return rows, nil
}

const journalEntrySelect = `SELECT idempotency_key,task_id,state,outcome,summary,result_ref,started_at,completed_at,attempt,result_id,lease_id,delivered_at FROM executions`

type journalRow interface {
	Scan(...any) error
}

func queryJournalEntry(row journalRow) (JournalEntry, string, bool, error) {
	var key, taskID string
	var state JournalState
	var outcome, summary, resultRef, resultID, leaseID sql.NullString
	var startedAt int64
	var completedAt, deliveredAt sql.NullInt64
	var attempt int
	if err := row.Scan(&key, &taskID, &state, &outcome, &summary, &resultRef, &startedAt, &completedAt, &attempt, &resultID, &leaseID, &deliveredAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return JournalEntry{}, "", false, nil
		}
		return JournalEntry{}, "", false, err
	}
	entry := JournalEntry{
		Key:       key,
		TaskID:    taskID,
		LeaseID:   leaseID.String,
		State:     state,
		Result:    edge.TaskResult{Outcome: edge.Outcome(outcome.String), Summary: summary.String, ResultRef: resultRef.String},
		Attempt:   attempt,
		ResultID:  resultID.String,
		Delivered: deliveredAt.Valid,
		StartedAt: time.Unix(startedAt, 0).UTC(),
	}
	if completedAt.Valid {
		entry.CompletedAt = time.Unix(completedAt.Int64, 0).UTC()
	}
	if entry.Attempt < 1 || (entry.LeaseID != "" && !journalLeasePattern.MatchString(entry.LeaseID)) || (entry.State != JournalStarted && entry.State != JournalCompleted) {
		return JournalEntry{}, "", false, errors.New("journal entry is invalid")
	}
	if entry.State == JournalStarted {
		if entry.Result != (edge.TaskResult{}) || entry.ResultID != "" || entry.Delivered || completedAt.Valid {
			return JournalEntry{}, "", false, errors.New("journal started entry is invalid")
		}
	} else if !validJournalResult(entry.Result) || !journalResultIDPattern.MatchString(entry.ResultID) || !completedAt.Valid {
		return JournalEntry{}, "", false, errors.New("journal completed entry is invalid")
	}
	return entry, taskID, true, nil
}

func validJournalResult(result edge.TaskResult) bool {
	if result.Summary == "" || len(result.Summary) > 2000 {
		return false
	}
	switch result.Outcome {
	case edge.OutcomeSucceeded, edge.OutcomeFailed, edge.OutcomeCancelled:
		return true
	default:
		return false
	}
}

func journalResultID(key, taskID string, result edge.TaskResult) string {
	body, _ := json.Marshal(struct {
		Version   int          `json:"version"`
		Key       string       `json:"key"`
		TaskID    string       `json:"task_id"`
		Outcome   edge.Outcome `json:"outcome"`
		Summary   string       `json:"summary"`
		ResultRef string       `json:"result_ref,omitempty"`
	}{Version: 1, Key: key, TaskID: taskID, Outcome: result.Outcome, Summary: result.Summary, ResultRef: result.ResultRef})
	sum := sha256.Sum256(body)
	return "jr_" + hex.EncodeToString(sum[:])
}

func (j *Journal) clock() time.Time {
	if j != nil && j.now != nil {
		return j.now()
	}
	return time.Now()
}

func (j *Journal) maximumEntries() int {
	if j != nil && j.maxEntries > 0 && j.maxEntries <= MaxJournalEntries {
		return j.maxEntries
	}
	return MaxJournalEntries
}
