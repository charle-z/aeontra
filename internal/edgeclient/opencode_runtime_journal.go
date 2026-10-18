//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const openCodeRuntimeJournalFile = "opencode-runtimes.db"

var goalDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type OpenCodeLocalState string

const (
	OpenCodeLocalStarting  OpenCodeLocalState = "starting"
	OpenCodeLocalRunning   OpenCodeLocalState = "running"
	OpenCodeLocalCompleted OpenCodeLocalState = "completed"
	OpenCodeLocalFailed    OpenCodeLocalState = "failed"
	OpenCodeLocalCancelled OpenCodeLocalState = "cancelled"
)

type OpenCodeRuntimeEntry struct {
	RuntimeID       string
	WorkspaceID     string
	GoalDigest      string
	ProviderProfile string
	State           OpenCodeLocalState
	ExitCode        int
	OutputTruncated bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OpenCodeRuntimeJournal struct {
	db  *sql.DB
	now func() time.Time
}

func OpenOpenCodeRuntimeJournal(stateRoot string) (*OpenCodeRuntimeJournal, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Clean(stateRoot), openCodeRuntimeJournalFile)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("OpenCode runtime journal is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("OpenCode runtime journal is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("OpenCode runtime journal is unavailable")
	}
	db.SetMaxOpenConns(1)
	journal := &OpenCodeRuntimeJournal{db: db, now: time.Now}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=4096`,
		`CREATE TABLE IF NOT EXISTS local_opencode_runtimes (
			runtime_id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			goal_digest TEXT NOT NULL,
			provider_profile TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('starting','running','completed','failed','cancelled')),
			exit_code INTEGER NOT NULL DEFAULT 0,
			output_truncated INTEGER NOT NULL DEFAULT 0 CHECK(output_truncated IN (0,1)),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE UNIQUE INDEX IF NOT EXISTS local_opencode_active_workspace
		 ON local_opencode_runtimes(workspace_id) WHERE state IN ('starting','running')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("OpenCode runtime journal initialization failed")
		}
	}
	if err := migrateOpenCodeRuntimeObjectiveUniqueness(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("OpenCode runtime journal permissions failed")
	}
	return journal, nil
}

func migrateOpenCodeRuntimeObjectiveUniqueness(db *sql.DB) error {
	var schema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='local_opencode_runtimes'`).Scan(&schema); err != nil {
		return errors.New("OpenCode runtime journal schema inspection failed")
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(schema), ""))
	if !strings.Contains(normalized, "unique(workspace_id,goal_digest)") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return errors.New("OpenCode runtime journal migration failed")
	}
	fail := func() error {
		_ = tx.Rollback()
		return errors.New("OpenCode runtime journal migration failed")
	}
	statements := []string{
		`DROP INDEX IF EXISTS local_opencode_active_workspace`,
		`ALTER TABLE local_opencode_runtimes RENAME TO local_opencode_runtimes_legacy`,
		`CREATE TABLE local_opencode_runtimes (
			runtime_id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			goal_digest TEXT NOT NULL,
			provider_profile TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('starting','running','completed','failed','cancelled')),
			exit_code INTEGER NOT NULL DEFAULT 0,
			output_truncated INTEGER NOT NULL DEFAULT 0 CHECK(output_truncated IN (0,1)),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`INSERT INTO local_opencode_runtimes(
			runtime_id,workspace_id,goal_digest,provider_profile,state,
			exit_code,output_truncated,created_at,updated_at
		) SELECT runtime_id,workspace_id,goal_digest,provider_profile,state,
			exit_code,output_truncated,created_at,updated_at
		FROM local_opencode_runtimes_legacy`,
		`DROP TABLE local_opencode_runtimes_legacy`,
		`CREATE UNIQUE INDEX local_opencode_active_workspace
		 ON local_opencode_runtimes(workspace_id) WHERE state IN ('starting','running')`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fail()
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("OpenCode runtime journal migration failed")
	}
	return nil
}

func (j *OpenCodeRuntimeJournal) Begin(ctx context.Context, runtimeID, workspaceID, goalDigest, providerProfile string) (OpenCodeRuntimeEntry, bool, error) {
	if j == nil || j.db == nil || !remoteRuntimeIDPattern.MatchString(runtimeID) || !workspaceIDPattern.MatchString(workspaceID) || !goalDigestPattern.MatchString(goalDigest) || providerProfile != remoteProviderProfile {
		return OpenCodeRuntimeEntry{}, false, errors.New("OpenCode runtime identity is invalid")
	}
	now := j.now().UTC()
	result, err := j.db.ExecContext(ctx, `INSERT OR IGNORE INTO local_opencode_runtimes(runtime_id,workspace_id,goal_digest,provider_profile,state,created_at,updated_at) VALUES(?,?,?,?,'starting',?,?)`, runtimeID, workspaceID, goalDigest, providerProfile, now.UnixNano(), now.UnixNano())
	if err != nil {
		return OpenCodeRuntimeEntry{}, false, errors.New("OpenCode runtime journal write failed")
	}
	rows, _ := result.RowsAffected()
	if rows == 1 {
		return OpenCodeRuntimeEntry{RuntimeID: runtimeID, WorkspaceID: workspaceID, GoalDigest: goalDigest, ProviderProfile: providerProfile, State: OpenCodeLocalStarting, CreatedAt: now, UpdatedAt: now}, true, nil
	}
	entry, readErr := j.Get(ctx, runtimeID)
	if readErr == nil {
		if entry.WorkspaceID != workspaceID || entry.GoalDigest != goalDigest || entry.ProviderProfile != providerProfile {
			return OpenCodeRuntimeEntry{}, false, errors.New("OpenCode runtime identity conflict")
		}
		return entry, false, nil
	}
	var activeRuntime string
	if err := j.db.QueryRowContext(ctx, `SELECT runtime_id FROM local_opencode_runtimes WHERE workspace_id=? AND state IN ('starting','running')`, workspaceID).Scan(&activeRuntime); err == nil {
		return OpenCodeRuntimeEntry{}, false, errors.New("workspace already has an active OpenCode runtime")
	}
	return OpenCodeRuntimeEntry{}, false, errors.New("OpenCode runtime journal conflict")
}

func (j *OpenCodeRuntimeJournal) MarkRunning(ctx context.Context, runtimeID string) error {
	return j.transition(ctx, runtimeID, OpenCodeLocalRunning, 0, false, OpenCodeLocalStarting, OpenCodeLocalRunning)
}

func (j *OpenCodeRuntimeJournal) Finish(ctx context.Context, runtimeID string, state OpenCodeLocalState, exitCode int, outputTruncated bool) error {
	if state != OpenCodeLocalCompleted && state != OpenCodeLocalFailed && state != OpenCodeLocalCancelled {
		return errors.New("OpenCode terminal state is invalid")
	}
	return j.transition(ctx, runtimeID, state, exitCode, outputTruncated, OpenCodeLocalStarting, OpenCodeLocalRunning, state)
}

func (j *OpenCodeRuntimeJournal) transition(ctx context.Context, runtimeID string, target OpenCodeLocalState, exitCode int, outputTruncated bool, allowed ...OpenCodeLocalState) error {
	if j == nil || j.db == nil || !remoteRuntimeIDPattern.MatchString(runtimeID) || len(allowed) == 0 {
		return errors.New("OpenCode runtime transition is invalid")
	}
	query := `UPDATE local_opencode_runtimes SET state=?,exit_code=?,output_truncated=?,updated_at=? WHERE runtime_id=? AND state IN (`
	args := []any{target, exitCode, boolInt(outputTruncated), j.now().UTC().UnixNano(), runtimeID}
	for index, state := range allowed {
		if index > 0 {
			query += ","
		}
		query += "?"
		args = append(args, state)
	}
	query += ")"
	result, err := j.db.ExecContext(ctx, query, args...)
	if err != nil {
		return errors.New("OpenCode runtime transition failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("OpenCode runtime transition conflict")
	}
	return nil
}

func (j *OpenCodeRuntimeJournal) Get(ctx context.Context, runtimeID string) (OpenCodeRuntimeEntry, error) {
	if j == nil || j.db == nil || !remoteRuntimeIDPattern.MatchString(runtimeID) {
		return OpenCodeRuntimeEntry{}, errors.New("OpenCode runtime id is invalid")
	}
	var entry OpenCodeRuntimeEntry
	var truncated int
	var createdAt, updatedAt int64
	if err := j.db.QueryRowContext(ctx, `SELECT runtime_id,workspace_id,goal_digest,provider_profile,state,exit_code,output_truncated,created_at,updated_at FROM local_opencode_runtimes WHERE runtime_id=?`, runtimeID).Scan(
		&entry.RuntimeID, &entry.WorkspaceID, &entry.GoalDigest, &entry.ProviderProfile, &entry.State, &entry.ExitCode, &truncated, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OpenCodeRuntimeEntry{}, errors.New("OpenCode runtime not found")
		}
		return OpenCodeRuntimeEntry{}, errors.New("OpenCode runtime journal read failed")
	}
	entry.OutputTruncated = truncated == 1
	entry.CreatedAt = time.Unix(0, createdAt).UTC()
	entry.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return entry, nil
}

func (j *OpenCodeRuntimeJournal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
