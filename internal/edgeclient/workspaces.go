package edgeclient

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const workspaceRegistryFile = "workspaces.db"

var workspaceIDPattern = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)

type Workspace struct {
	ID        string    `json:"workspace_id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WorkspaceRegistry struct {
	db  *sql.DB
	now func() time.Time
}

func OpenWorkspaceRegistry(stateRoot string) (*WorkspaceRegistry, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Clean(stateRoot), workspaceRegistryFile)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("workspace registry is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("workspace registry is unavailable")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("workspace registry is unavailable")
	}
	db.SetMaxOpenConns(1)
	registry := &WorkspaceRegistry{db: db, now: time.Now}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=4096`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			workspace_id TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("workspace registry initialization failed")
		}
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, errors.New("workspace registry permissions failed")
	}
	return registry, nil
}

func (r *WorkspaceRegistry) Add(path string) (Workspace, bool, error) {
	if r == nil || r.db == nil {
		return Workspace{}, false, errors.New("workspace registry is unavailable")
	}
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return Workspace{}, false, err
	}
	if existing, err := r.byPath(validated); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, false, errors.New("workspace registry lookup failed")
	}
	id, err := newWorkspaceID()
	if err != nil {
		return Workspace{}, false, errors.New("workspace id generation failed")
	}
	now := r.now().UTC()
	if _, err := r.db.Exec(`INSERT INTO workspaces(workspace_id,path,created_at,updated_at) VALUES(?,?,?,?)`, id, validated, now.UnixNano(), now.UnixNano()); err != nil {
		if existing, readErr := r.byPath(validated); readErr == nil {
			return existing, false, nil
		}
		return Workspace{}, false, errors.New("workspace registration failed")
	}
	return Workspace{ID: id, Path: validated, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (r *WorkspaceRegistry) List() ([]Workspace, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workspace registry is unavailable")
	}
	rows, err := r.db.Query(`SELECT workspace_id,path,created_at,updated_at FROM workspaces ORDER BY created_at,workspace_id`)
	if err != nil {
		return nil, errors.New("workspace registry read failed")
	}
	defer rows.Close()
	items := make([]Workspace, 0)
	for rows.Next() {
		item, err := scanWorkspace(rows)
		if err != nil {
			return nil, errors.New("workspace registry read failed")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("workspace registry read failed")
	}
	return items, nil
}

func (r *WorkspaceRegistry) Remove(id string) error {
	if r == nil || r.db == nil || !workspaceIDPattern.MatchString(id) {
		return errors.New("workspace id is invalid")
	}
	result, err := r.db.Exec(`DELETE FROM workspaces WHERE workspace_id=?`, id)
	if err != nil {
		return errors.New("workspace removal failed")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("workspace not found")
	}
	return nil
}

func (r *WorkspaceRegistry) Resolve(id string) (string, error) {
	if r == nil || r.db == nil || !workspaceIDPattern.MatchString(id) {
		return "", errors.New("workspace id is invalid")
	}
	var path string
	if err := r.db.QueryRow(`SELECT path FROM workspaces WHERE workspace_id=?`, id).Scan(&path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("workspace not found")
		}
		return "", errors.New("workspace registry read failed")
	}
	return ValidateRegisteredWorkspace(path)
}

func (r *WorkspaceRegistry) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *WorkspaceRegistry) byPath(path string) (Workspace, error) {
	row := r.db.QueryRow(`SELECT workspace_id,path,created_at,updated_at FROM workspaces WHERE path=?`, path)
	return scanWorkspace(row)
}

func scanWorkspace(scanner interface{ Scan(...any) error }) (Workspace, error) {
	var item Workspace
	var createdAt, updatedAt int64
	if err := scanner.Scan(&item.ID, &item.Path, &createdAt, &updatedAt); err != nil {
		return Workspace{}, err
	}
	item.CreatedAt = time.Unix(0, createdAt).UTC()
	item.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return item, nil
}

func ValidateRegisteredWorkspace(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) || path == string(os.PathSeparator) || isWindowsMount(path) {
		return "", errors.New("workspace path is unsafe")
	}
	if err := rejectSymlinkPath(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("workspace path is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != path {
		return "", errors.New("workspace path is unsafe")
	}
	if err := requireCurrentOwner(info); err != nil {
		return "", err
	}
	return path, nil
}

func newWorkspaceID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "ws_" + hex.EncodeToString(value), nil
}
