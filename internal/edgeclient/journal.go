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
	Key         string
	TaskID      string
	LeaseID     string
	State       JournalState
	Result      edge.TaskResult
	New         bool
	Attempt     int
	ResultID    string
	Delivered   bool
	StartedAt   time.Time
	CompletedAt time.Time
}

type Journal struct {
	db         *sql.DB
	now        func() time.Time
	maxEntries int
}

var journalKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
var journalTaskPattern = regexp.MustCompile(`^et_[a-f0-9]{32}$`)

const MaxJournalEntries = 4096

func OpenJournal(stateRoot string) (*Journal, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(stateRoot, "journal.db")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() <= 0 || info.Size() > 32<<20 {
			return nil, errors.New("edge journal is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("edge journal unavailable")
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
		`CREATE TABLE IF NOT EXISTS executions(idempotency_key TEXT PRIMARY KEY, task_id TEXT NOT NULL, state TEXT NOT NULL, outcome TEXT, summary TEXT, result_ref TEXT, started_at INTEGER NOT NULL, completed_at INTEGER, attempt INTEGER NOT NULL DEFAULT 1, result_id TEXT, lease_id TEXT, delivered_at INTEGER, updated_at INTEGER NOT NULL DEFAULT 0) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("edge journal initialization failed")
		}
	}
	if err := migrateJournalV2(db); err != nil {
		_ = db.Close()
		return nil, errors.New("edge journal migration failed")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("edge journal permissions failed")
	}
	return &Journal{db: db, now: time.Now, maxEntries: MaxJournalEntries}, nil
}

func (j *Journal) Begin(key, taskID string) (JournalEntry, error) {
	return j.BeginAttempt(key, taskID, 1)
}

func (j *Journal) Finish(key, taskID string, result edge.TaskResult) error {
	_, err := j.FinishEntry(key, taskID, result)
	return err
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
