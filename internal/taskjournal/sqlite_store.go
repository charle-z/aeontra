package taskjournal

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultPageSize         = 50
	MaxPageSize             = 200
	MaxRecords              = 10_000
	MaxEvents               = 20_000
	TerminalRetention       = 30 * 24 * time.Hour
	EventRetention          = 30 * 24 * time.Hour
	TargetMaxBytes    int64 = 64 << 20
)

const (
	storagePruneTargetBytes = TargetMaxBytes * 7 / 8
	storagePruneBatch       = 500
)

type SQLiteStore struct {
	root string
	path string
	db   *sql.DB
	mu   sync.Mutex
}

func OpenSQLiteStore(root string) (*SQLiteStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("task journal: root must be absolute")
	}
	if err := prepareSQLiteRoot(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "tasks.db")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("task journal: database path is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("task journal: database path is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("task journal: database is unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{root: root, path: path, db: db}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("task journal: cannot secure database")
	}
	if err := store.migrateLegacy(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.Prune(time.Now().UTC()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func prepareSQLiteRoot(root string) error {
	clean := filepath.Clean(root)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}
	parts := strings.Split(rest, string(os.PathSeparator))
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Join(append([]string{current}, parts[index+1:]...)...), 0o700); err != nil {
				return errors.New("task journal: cannot create root")
			}
			break
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("task journal: root ancestry is unsafe")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New("task journal: cannot create root")
	}
	info, err := os.Lstat(root)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("task journal: root must be a real directory")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return errors.New("task journal: cannot secure root")
	}
	return nil
}

func (s *SQLiteStore) initialize() error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA auto_vacuum=INCREMENTAL`,
		`PRAGMA wal_autocheckpoint=1000`,
		`PRAGMA max_page_count=16384`,
		`CREATE TABLE IF NOT EXISTS tasks (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL UNIQUE,
			controller TEXT NOT NULL,
			operation TEXT NOT NULL,
			safe_summary TEXT NOT NULL,
			project_id TEXT NOT NULL DEFAULT '',
			edge_id TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			heartbeat_at INTEGER NOT NULL,
			terminal_at INTEGER,
			version INTEGER NOT NULL CHECK(version >= 1)
		)`,
		`CREATE INDEX IF NOT EXISTS tasks_order ON tasks(updated_at DESC,sequence DESC,task_id DESC)`,
		`CREATE INDEX IF NOT EXISTS tasks_retention ON tasks(terminal_at,updated_at)`,
		`CREATE TABLE IF NOT EXISTS task_events (
			event_id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			task_version INTEGER NOT NULL,
			sequence INTEGER NOT NULL,
			occurred_at INTEGER NOT NULL,
			event_type TEXT NOT NULL CHECK(event_type IN ('started','heartbeat','transition')),
			state TEXT NOT NULL,
			operation TEXT NOT NULL,
			FOREIGN KEY(task_id) REFERENCES tasks(task_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS task_events_replay ON task_events(event_id)`,
		`CREATE INDEX IF NOT EXISTS task_events_filter ON task_events(state,event_type,operation,event_id DESC)`,
		`CREATE INDEX IF NOT EXISTS tasks_controller ON tasks(controller,task_id)`,
		`CREATE TABLE IF NOT EXISTS journal_meta (key TEXT PRIMARY KEY,value TEXT NOT NULL) WITHOUT ROWID`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("task journal: database initialization failed")
		}
	}
	return s.ensureScopeSchema()
}

func (s *SQLiteStore) ensureScopeSchema() error {
	rows, err := s.db.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		return errors.New("task journal: scope schema unavailable")
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return errors.New("task journal: scope schema invalid")
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return errors.New("task journal: scope schema unavailable")
	}
	for _, column := range []string{"project_id", "edge_id"} {
		if columns[column] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE tasks ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
			return errors.New("task journal: scope migration failed")
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS tasks_project_scope ON tasks(project_id,updated_at DESC,sequence DESC,task_id DESC)`,
		`CREATE INDEX IF NOT EXISTS tasks_edge_scope ON tasks(edge_id,updated_at DESC,sequence DESC,task_id DESC)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("task journal: scope index failed")
		}
	}
	return nil
}

type legacyEntry struct {
	TaskID     string    `json:"task_id"`
	Operation  string    `json:"operation"`
	Summary    string    `json:"summary"`
	State      State     `json:"state"`
	Heartbeat  time.Time `json:"heartbeat"`
	Controller string    `json:"controller"`
}

func (s *SQLiteStore) migrateLegacy() error {
	var marker string
	err := s.db.QueryRow(`SELECT value FROM journal_meta WHERE key='legacy_json_migration'`).Scan(&marker)
	if err == nil && marker == "complete" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("task journal: migration state unavailable")
	}
	items, err := os.ReadDir(s.root)
	if err != nil {
		return errors.New("task journal: cannot inspect legacy entries")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return errors.New("task journal: migration transaction failed")
	}
	defer tx.Rollback()
	migrated := make([]string, 0)
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.root, item.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxEntryFileSize {
			continue
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return errors.New("task journal: cannot read legacy entry")
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var old legacyEntry
		if decoder.Decode(&old) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			continue
		}
		entry := Entry{
			TaskID: old.TaskID, Controller: old.Controller, Operation: old.Operation,
			Summary: old.Summary, State: old.State, CreatedAt: old.Heartbeat.UTC(),
			UpdatedAt: old.Heartbeat.UTC(), HeartbeatAt: old.Heartbeat.UTC(),
			Heartbeat: old.Heartbeat.UTC(), Version: 1,
		}
		if isTerminal(old.State) {
			terminal := old.Heartbeat.UTC()
			entry.TerminalAt = &terminal
		}
		if entry.validate() != nil {
			continue
		}
		result, err := tx.Exec(`INSERT OR IGNORE INTO tasks(task_id,controller,operation,safe_summary,project_id,edge_id,state,created_at,updated_at,heartbeat_at,terminal_at,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			entry.TaskID, entry.Controller, entry.Operation, entry.Summary, entry.ProjectID, entry.EdgeID, entry.State,
			unixNano(entry.CreatedAt), unixNano(entry.UpdatedAt), unixNano(entry.HeartbeatAt), nullableUnixNano(entry.TerminalAt), entry.Version)
		if err != nil {
			return errors.New("task journal: legacy import failed")
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return errors.New("task journal: legacy import result failed")
		}
		if rows == 1 {
			entry.Sequence, _ = result.LastInsertId()
			eventType := EventStarted
			if isTerminal(entry.State) {
				eventType = EventTransition
			}
			if _, err := tx.Exec(`INSERT INTO task_events(task_id,task_version,sequence,occurred_at,event_type,state,operation) VALUES(?,?,?,?,?,?,?)`,
				entry.TaskID, entry.Version, entry.Sequence, unixNano(entry.UpdatedAt), eventType, entry.State, entry.Operation); err != nil {
				return errors.New("task journal: legacy event import failed")
			}
		}
		migrated = append(migrated, path)
	}
	if _, err := tx.Exec(`INSERT INTO journal_meta(key,value) VALUES('legacy_json_migration','complete') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		return errors.New("task journal: migration marker failed")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("task journal: migration commit failed")
	}
	if len(migrated) == 0 {
		return nil
	}
	archive := filepath.Join(s.root, "legacy-archive")
	if err := os.MkdirAll(archive, 0o700); err != nil {
		return nil
	}
	_ = os.Chmod(archive, 0o700)
	for _, path := range migrated {
		_ = os.Rename(path, filepath.Join(archive, filepath.Base(path)))
	}
	return nil
}

func (s *SQLiteStore) Create(entry Entry) (Entry, Event, error) {
	if s == nil || s.db == nil {
		return Entry{}, Event{}, errors.New("task journal: store is unavailable")
	}
	if err := entry.validate(); err != nil {
		return Entry{}, Event{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.pruneLocked(entry.UpdatedAt); err != nil {
		return Entry{}, Event{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, Event{}, errors.New("task journal: write transaction failed")
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO tasks(task_id,controller,operation,safe_summary,project_id,edge_id,state,created_at,updated_at,heartbeat_at,terminal_at,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.TaskID, entry.Controller, entry.Operation, entry.Summary, entry.ProjectID, entry.EdgeID, entry.State,
		unixNano(entry.CreatedAt), unixNano(entry.UpdatedAt), unixNano(entry.HeartbeatAt), nullableUnixNano(entry.TerminalAt), entry.Version)
	if err != nil {
		return Entry{}, Event{}, errors.New("task journal: task already exists or storage is full")
	}
	entry.Sequence, _ = result.LastInsertId()
	event, err := insertSQLiteEvent(tx, entry, EventStarted)
	if err != nil {
		return Entry{}, Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, Event{}, errors.New("task journal: write commit failed")
	}
	return entry, event, nil
}

func (s *SQLiteStore) Update(taskID string, state *State, now time.Time) (Entry, Event, error) {
	if s == nil || !taskIDPattern.MatchString(taskID) {
		return Entry{}, Event{}, errors.New("task journal: invalid task id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.pruneLocked(now.UTC()); err != nil {
		return Entry{}, Event{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, Event{}, errors.New("task journal: update transaction failed")
	}
	defer tx.Rollback()
	entry, ok, err := getSQLiteEntry(tx, taskID)
	if err != nil || !ok {
		if err != nil {
			return Entry{}, Event{}, err
		}
		return Entry{}, Event{}, errors.New("task journal: task not found")
	}
	if isTerminal(entry.State) {
		if state == nil {
			return entry, Event{}, nil
		}
		return Entry{}, Event{}, errors.New("task journal: terminal task cannot transition")
	}
	now = now.UTC()
	entry.UpdatedAt = now
	entry.HeartbeatAt = now
	entry.Heartbeat = now
	entry.Version++
	eventType := EventHeartbeat
	if state != nil {
		entry.State = *state
		eventType = EventTransition
		if isTerminal(entry.State) {
			terminal := now
			entry.TerminalAt = &terminal
		}
	}
	if _, err := tx.Exec(`UPDATE tasks SET state=?,updated_at=?,heartbeat_at=?,terminal_at=?,version=? WHERE task_id=?`,
		entry.State, unixNano(entry.UpdatedAt), unixNano(entry.HeartbeatAt), nullableUnixNano(entry.TerminalAt), entry.Version, entry.TaskID); err != nil {
		return Entry{}, Event{}, errors.New("task journal: update failed")
	}
	event, err := insertSQLiteEvent(tx, entry, eventType)
	if err != nil {
		return Entry{}, Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, Event{}, errors.New("task journal: update commit failed")
	}
	return entry, event, nil
}

type sqliteQueryer interface {
	QueryRow(string, ...any) *sql.Row
}

func getSQLiteEntry(q sqliteQueryer, taskID string) (Entry, bool, error) {
	row := q.QueryRow(`SELECT sequence,task_id,controller,operation,safe_summary,project_id,edge_id,state,created_at,updated_at,heartbeat_at,terminal_at,version FROM tasks WHERE task_id=?`, taskID)
	entry, err := scanSQLiteEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, errors.New("task journal: task read failed")
	}
	return entry, true, nil
}

func insertSQLiteEvent(tx *sql.Tx, entry Entry, eventType string) (Event, error) {
	result, err := tx.Exec(`INSERT INTO task_events(task_id,task_version,sequence,occurred_at,event_type,state,operation) VALUES(?,?,?,?,?,?,?)`,
		entry.TaskID, entry.Version, entry.Sequence, unixNano(entry.UpdatedAt), eventType, entry.State, entry.Operation)
	if err != nil {
		return Event{}, errors.New("task journal: event write failed")
	}
	id, _ := result.LastInsertId()
	return Event{EventID: id, TaskID: entry.TaskID, TaskVersion: entry.Version, Sequence: entry.Sequence, OccurredAt: entry.UpdatedAt, EventType: eventType, State: entry.State, Operation: entry.Operation, Task: entry}, nil
}

func (s *SQLiteStore) Get(taskID string) (Entry, bool, error) {
	if s == nil || !taskIDPattern.MatchString(taskID) {
		return Entry{}, false, errors.New("task journal: invalid task id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return getSQLiteEntry(s.db, taskID)
}

func (s *SQLiteStore) ListPage(limit int, cursor string) (Page, error) {
	return s.ListPageFiltered(limit, cursor, TaskFilter{})
}

func (s *SQLiteStore) ListPageFiltered(limit int, cursor string, filter TaskFilter) (Page, error) {
	if s == nil || s.db == nil {
		return Page{}, errors.New("task journal: store is unavailable")
	}
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize {
		return Page{}, errors.New("task journal: invalid limit")
	}
	if err := filter.validate(); err != nil {
		return Page{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `SELECT sequence,task_id,controller,operation,safe_summary,project_id,edge_id,state,created_at,updated_at,heartbeat_at,terminal_at,version FROM tasks`
	conditions := make([]string, 0, 6)
	args := make([]any, 0, 16)
	if filter.Controller != "" {
		conditions = append(conditions, `controller=?`)
		args = append(args, filter.Controller)
	}
	if filter.State != "" {
		conditions = append(conditions, `state=?`)
		args = append(args, filter.State)
	}
	if filter.Operation != "" {
		conditions = append(conditions, `operation=?`)
		args = append(args, filter.Operation)
	}
	if filter.ProjectID != "" {
		conditions = append(conditions, `project_id=?`)
		args = append(args, filter.ProjectID)
	}
	if filter.EdgeID != "" {
		conditions = append(conditions, `edge_id=?`)
		args = append(args, filter.EdgeID)
	}
	if cursor != "" {
		updated, sequence, taskID, err := decodeTaskCursor(cursor)
		if err != nil {
			return Page{}, err
		}
		conditions = append(conditions, `(updated_at < ? OR (updated_at = ? AND sequence < ?) OR (updated_at = ? AND sequence = ? AND task_id < ?))`)
		args = append(args, updated, updated, sequence, updated, sequence, taskID)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY updated_at DESC,sequence DESC,task_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return Page{}, errors.New("task journal: list failed")
	}
	defer rows.Close()
	entries := make([]Entry, 0, limit+1)
	for rows.Next() {
		entry, err := scanSQLiteEntry(rows)
		if err != nil {
			return Page{}, errors.New("task journal: list result failed")
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{}, errors.New("task journal: list iteration failed")
	}
	page := Page{Entries: entries}
	if len(entries) > limit {
		page.HasMore = true
		page.Entries = entries[:limit]
		page.NextCursor = encodeTaskCursor(page.Entries[len(page.Entries)-1])
	}
	return page, nil
}

type sqliteScanner interface{ Scan(...any) error }

func scanSQLiteEntry(row sqliteScanner) (Entry, error) {
	var entry Entry
	var created, updated, heartbeat int64
	var terminal sql.NullInt64
	if err := row.Scan(&entry.Sequence, &entry.TaskID, &entry.Controller, &entry.Operation, &entry.Summary, &entry.ProjectID, &entry.EdgeID, &entry.State,
		&created, &updated, &heartbeat, &terminal, &entry.Version); err != nil {
		return Entry{}, err
	}
	entry.CreatedAt = time.Unix(0, created).UTC()
	entry.UpdatedAt = time.Unix(0, updated).UTC()
	entry.HeartbeatAt = time.Unix(0, heartbeat).UTC()
	entry.Heartbeat = entry.HeartbeatAt
	if terminal.Valid {
		value := time.Unix(0, terminal.Int64).UTC()
		entry.TerminalAt = &value
	}
	return entry, entry.validate()
}

func encodeTaskCursor(entry Entry) string {
	raw := fmt.Sprintf("%d:%d:%s", unixNano(entry.UpdatedAt), entry.Sequence, entry.TaskID)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeTaskCursor(cursor string) (int64, int64, string, error) {
	body, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(body) > 160 {
		return 0, 0, "", errors.New("task journal: invalid cursor")
	}
	parts := strings.Split(string(body), ":")
	if len(parts) != 3 || !taskIDPattern.MatchString(parts[2]) {
		return 0, 0, "", errors.New("task journal: invalid cursor")
	}
	updated, err1 := strconv.ParseInt(parts[0], 10, 64)
	sequence, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || updated <= 0 || sequence <= 0 {
		return 0, 0, "", errors.New("task journal: invalid cursor")
	}
	return updated, sequence, parts[2], nil
}

func (s *SQLiteStore) Replay(after int64, limit int) ([]Event, bool, error) {
	if limit <= 0 || limit > MaxPageSize {
		limit = MaxPageSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var minID, maxID sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(event_id),MAX(event_id) FROM task_events`).Scan(&minID, &maxID); err != nil {
		return nil, false, errors.New("task journal: replay bounds failed")
	}
	gap := after > 0 && (!maxID.Valid || after > maxID.Int64 || (minID.Valid && after < minID.Int64-1))
	if gap {
		return nil, true, nil
	}
	rows, err := s.db.Query(`SELECT e.event_id,e.task_id,e.task_version,e.sequence,e.occurred_at,e.event_type,e.state,e.operation,
		t.controller,t.safe_summary,t.project_id,t.edge_id,t.created_at,t.updated_at,t.heartbeat_at,t.terminal_at,t.version
		FROM task_events e JOIN tasks t ON t.task_id=e.task_id WHERE e.event_id>? ORDER BY e.event_id LIMIT ?`, after, limit)
	if err != nil {
		return nil, false, errors.New("task journal: replay failed")
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var occurred, created, updated, heartbeat int64
		var terminal sql.NullInt64
		if err := rows.Scan(&event.EventID, &event.TaskID, &event.TaskVersion, &event.Sequence, &occurred, &event.EventType, &event.State, &event.Operation,
			&event.Task.Controller, &event.Task.Summary, &event.Task.ProjectID, &event.Task.EdgeID, &created, &updated, &heartbeat, &terminal, &event.Task.Version); err != nil {
			return nil, false, errors.New("task journal: replay result failed")
		}
		event.OccurredAt = time.Unix(0, occurred).UTC()
		event.Task.TaskID = event.TaskID
		event.Task.Sequence = event.Sequence
		event.Task.Operation = event.Operation
		event.Task.State = event.State
		event.Task.CreatedAt = time.Unix(0, created).UTC()
		event.Task.UpdatedAt = time.Unix(0, updated).UTC()
		event.Task.HeartbeatAt = time.Unix(0, heartbeat).UTC()
		event.Task.Heartbeat = event.Task.HeartbeatAt
		if terminal.Valid {
			value := time.Unix(0, terminal.Int64).UTC()
			event.Task.TerminalAt = &value
		}
		events = append(events, event)
	}
	return events, false, rows.Err()
}

func (s *SQLiteStore) Prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pruneLocked(now)
}

func (s *SQLiteStore) pruneLocked(now time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM tasks WHERE terminal_at IS NOT NULL AND terminal_at < ?`, unixNano(now.Add(-TerminalRetention))); err != nil {
		return errors.New("task journal: retention prune failed")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil {
		return errors.New("task journal: count failed")
	}
	if excess := count - MaxRecords; excess > 0 {
		if _, err := s.db.Exec(`DELETE FROM tasks WHERE task_id IN (SELECT task_id FROM tasks WHERE terminal_at IS NOT NULL ORDER BY updated_at ASC,sequence ASC LIMIT ?)`, excess); err != nil {
			return errors.New("task journal: count prune failed")
		}
	}
	if _, err := s.db.Exec(`DELETE FROM task_events WHERE occurred_at < ?`, unixNano(now.Add(-EventRetention))); err != nil {
		return errors.New("task journal: event retention prune failed")
	}
	if _, err := s.db.Exec(`DELETE FROM task_events WHERE event_id NOT IN (SELECT event_id FROM task_events ORDER BY event_id DESC LIMIT ?)`, MaxEvents); err != nil {
		return errors.New("task journal: event count prune failed")
	}
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	_, _ = s.db.Exec(`PRAGMA incremental_vacuum(128)`)
	if err := s.pruneBytesLocked(storagePruneTargetBytes); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) pruneBytesLocked(maxUsedBytes int64) error {
	if maxUsedBytes <= 0 {
		return errors.New("task journal: invalid storage prune target")
	}
	for attempt := 0; attempt < 256; attempt++ {
		used, err := s.sqliteUsedBytesLocked()
		if err != nil {
			return err
		}
		if used <= maxUsedBytes {
			return nil
		}

		result, err := s.db.Exec(`DELETE FROM task_events WHERE event_id IN (
			SELECT event_id FROM task_events ORDER BY event_id ASC LIMIT ?
		)`, storagePruneBatch)
		if err != nil {
			return errors.New("task journal: byte event prune failed")
		}
		deleted, _ := result.RowsAffected()
		if deleted == 0 {
			result, err = s.db.Exec(`DELETE FROM tasks WHERE task_id IN (
				SELECT task_id FROM tasks WHERE terminal_at IS NOT NULL
				ORDER BY updated_at ASC,sequence ASC,task_id ASC LIMIT ?
			)`, storagePruneBatch)
			if err != nil {
				return errors.New("task journal: byte task prune failed")
			}
			deleted, _ = result.RowsAffected()
		}
		if deleted == 0 {
			return errors.New("task journal: storage budget exhausted by active records")
		}
		_, _ = s.db.Exec(`PRAGMA incremental_vacuum(256)`)
	}
	return errors.New("task journal: storage prune did not converge")
}

func (s *SQLiteStore) sqliteUsedBytesLocked() (int64, error) {
	var pageSize, pageCount, freePages int64
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, errors.New("task journal: page size unavailable")
	}
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, errors.New("task journal: page count unavailable")
	}
	if err := s.db.QueryRow(`PRAGMA freelist_count`).Scan(&freePages); err != nil {
		return 0, errors.New("task journal: free page count unavailable")
	}
	if pageSize <= 0 || pageCount < 0 || freePages < 0 || freePages > pageCount {
		return 0, errors.New("task journal: page accounting invalid")
	}
	return pageSize * (pageCount - freePages), nil
}

func (s *SQLiteStore) Status(detail string) Status {
	status := Status{Storage: StorageHealthy, Detail: detail}
	if s == nil || s.db == nil {
		status.Storage = StorageDegraded
		status.Detail = "journal unavailable"
		return status
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&status.RecordCount)
	status.DatabaseSize = sqliteFileSize(s.path)
	status.WALSize = sqliteFileSize(s.path + "-wal")
	total := status.DatabaseSize + status.WALSize + sqliteFileSize(s.path+"-shm")
	if total >= TargetMaxBytes*9/10 {
		status.Storage = StorageDegraded
		if status.Detail == "" {
			status.Detail = "storage hard cap is near"
		}
	} else if total >= TargetMaxBytes*3/4 {
		status.Storage = StorageNearingLimit
		if status.Detail == "" {
			status.Detail = "storage budget is nearing limit"
		}
	}
	return status
}

func sqliteFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

func unixNano(value time.Time) int64 { return value.UTC().UnixNano() }

func nullableUnixNano(value *time.Time) any {
	if value == nil {
		return nil
	}
	return unixNano(*value)
}
