package edgeclient

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"

	_ "modernc.org/sqlite"
)

type JournalState string

const (
	JournalStarted   JournalState = "started"
	JournalCompleted JournalState = "completed"
)

type JournalEntry struct {
	State  JournalState
	Result edge.TaskResult
	New    bool
}

type Journal struct {
	db *sql.DB
}

var journalKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var journalTaskPattern = regexp.MustCompile(`^et_[a-f0-9]{32}$`)

func OpenJournal(stateRoot string) (*Journal, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(stateRoot, "journal.db")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("edge journal is unsafe")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("edge journal unavailable")
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=4096`,
		`CREATE TABLE IF NOT EXISTS executions(idempotency_key TEXT PRIMARY KEY, task_id TEXT NOT NULL, state TEXT NOT NULL, outcome TEXT, summary TEXT, result_ref TEXT, started_at INTEGER NOT NULL, completed_at INTEGER) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("edge journal initialization failed")
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("edge journal permissions failed")
	}
	return &Journal{db: db}, nil
}

func (j *Journal) Begin(key, taskID string) (JournalEntry, error) {
	if !journalKeyPattern.MatchString(key) || !journalTaskPattern.MatchString(taskID) {
		return JournalEntry{}, errors.New("journal identity is invalid")
	}
	result, err := j.db.Exec(`INSERT OR IGNORE INTO executions(idempotency_key,task_id,state,started_at) VALUES(?,?,?,?)`, key, taskID, JournalStarted, time.Now().UTC().Unix())
	if err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	rows, _ := result.RowsAffected()
	var storedTask string
	var state JournalState
	var outcome, summary, resultRef sql.NullString
	if err := j.db.QueryRow(`SELECT task_id,state,outcome,summary,result_ref FROM executions WHERE idempotency_key=?`, key).Scan(&storedTask, &state, &outcome, &summary, &resultRef); err != nil {
		return JournalEntry{}, errors.New("edge journal unavailable")
	}
	if storedTask != taskID {
		return JournalEntry{}, errors.New("journal idempotency conflict")
	}
	return JournalEntry{State: state, Result: edge.TaskResult{Outcome: edge.Outcome(outcome.String), Summary: summary.String, ResultRef: resultRef.String}, New: rows == 1}, nil
}

func (j *Journal) Finish(key, taskID string, result edge.TaskResult) error {
	if !journalKeyPattern.MatchString(key) || !journalTaskPattern.MatchString(taskID) || result.Outcome == "" || result.Summary == "" {
		return errors.New("journal completion is invalid")
	}
	existing, err := j.Begin(key, taskID)
	if err != nil {
		return err
	}
	if existing.State == JournalCompleted {
		if existing.Result == result {
			return nil
		}
		return errors.New("journal completion conflicts")
	}
	update, err := j.db.Exec(`UPDATE executions SET state=?,outcome=?,summary=?,result_ref=?,completed_at=? WHERE idempotency_key=? AND task_id=? AND state=?`, JournalCompleted, result.Outcome, result.Summary, nullableJournalString(result.ResultRef), time.Now().UTC().Unix(), key, taskID, JournalStarted)
	if err != nil {
		return errors.New("edge journal unavailable")
	}
	rows, _ := update.RowsAffected()
	if rows != 1 {
		return errors.New("journal completion failed")
	}
	return nil
}

func (j *Journal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

func nullableJournalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
