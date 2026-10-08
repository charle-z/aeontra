package edgeclient

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	_ "modernc.org/sqlite"
)

const workspaceRegistryFile = "workspaces.db"

var workspaceIDPattern = regexp.MustCompile(`^ws_[a-f0-9]{32}$`)

type Workspace struct {
	ID   string `json:"workspace_id"`
	Path string `json:"path"`
	// WindowsDevRoot is the registry-derived root retained for Windows
	// foreground execution. It is intentionally not part of the public record.
	WindowsDevRoot        string           `json:"-"`
	Profile               WorkspaceProfile `json:"profile"`
	Mode                  WorkspaceMode    `json:"mode"`
	MachineName           string           `json:"machine_name,omitempty"`
	TargetIP              string           `json:"target_ip,omitempty"`
	Difficulty            string           `json:"difficulty,omitempty"`
	OS                    string           `json:"os,omitempty"`
	VPNInterface          string           `json:"vpn_interface,omitempty"`
	AuthorizationRevision uint64           `json:"authorization_revision"`
	NetworkPosture        string           `json:"network_posture"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

type WorkspaceRegistry struct {
	db    *sql.DB
	now   func() time.Time
	roots WorkspaceRoots
}

func OpenWorkspaceRegistry(stateRoot string) (*WorkspaceRegistry, error) {
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Clean(stateRoot), workspaceRegistryFile)
	if _, err := os.Lstat(path); err == nil {
		if err := requirePrivateRegularFile(path); err != nil {
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
	roots := defaultWorkspaceRoots()
	if err := validateWorkspaceStateSeparation(stateRoot, roots); err != nil {
		_ = db.Close()
		return nil, errors.New("workspace roots overlap private Edge state")
	}
	registry := &WorkspaceRegistry{db: db, now: time.Now, roots: roots}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=4096`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			workspace_id TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS workspace_configs (
			workspace_id TEXT PRIMARY KEY REFERENCES workspaces(workspace_id) ON DELETE CASCADE,
			profile TEXT NOT NULL,
			mode TEXT NOT NULL,
			machine_name TEXT NOT NULL DEFAULT '',
			target_ip TEXT NOT NULL DEFAULT '',
			difficulty TEXT NOT NULL DEFAULT '',
			os TEXT NOT NULL DEFAULT '',
			vpn_interface TEXT NOT NULL DEFAULT ''
		) WITHOUT ROWID`,
		`INSERT OR IGNORE INTO workspace_configs(workspace_id,profile,mode) SELECT workspace_id,'sandbox','dev' FROM workspaces`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, errors.New("workspace registry initialization failed")
		}
	}
	if err := ensureWorkspaceAuthorizationRevision(db); err != nil {
		_ = db.Close()
		return nil, errors.New("workspace registry migration failed")
	}
	if err := securePrivateRegularPath(path); err != nil {
		_ = db.Close()
		return nil, errors.New("workspace registry permissions failed")
	}
	return registry, nil
}

func (r *WorkspaceRegistry) Add(path string) (Workspace, bool, error) {
	return r.AddProfile(path, WorkspaceProfileSandbox)
}

func (r *WorkspaceRegistry) List() ([]Workspace, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workspace registry is unavailable")
	}
	rows, err := r.db.Query(workspaceSelect + ` ORDER BY w.created_at,w.workspace_id`)
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
		item = r.decorateWorkspace(item)
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
	workspace, err := r.Get(id)
	if err != nil {
		return "", err
	}
	return workspace.Path, nil
}

func (r *WorkspaceRegistry) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *WorkspaceRegistry) byPath(path string) (Workspace, error) {
	row := r.db.QueryRow(workspaceSelect+` WHERE w.path=?`, path)
	item, err := scanWorkspace(row)
	if err != nil {
		return Workspace{}, err
	}
	return r.decorateWorkspace(item), nil
}

func (r *WorkspaceRegistry) decorateWorkspace(workspace Workspace) Workspace {
	if workspace.Profile == WorkspaceProfileWindowsWorkcell {
		workspace.WindowsDevRoot = r.roots.WindowsDev
	}
	return workspace
}

func scanWorkspace(scanner interface{ Scan(...any) error }) (Workspace, error) {
	var item Workspace
	var createdAt, updatedAt int64
	if err := scanner.Scan(&item.ID, &item.Path, &createdAt, &updatedAt, &item.Profile, &item.Mode, &item.MachineName, &item.TargetIP, &item.Difficulty, &item.OS, &item.VPNInterface, &item.AuthorizationRevision); err != nil {
		return Workspace{}, err
	}
	item.CreatedAt = time.Unix(0, createdAt).UTC()
	item.UpdatedAt = time.Unix(0, updatedAt).UTC()
	item.NetworkPosture = networkPosture(item.Profile)
	return item, nil
}

func ensureWorkspaceAuthorizationRevision(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(workspace_configs)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "authorization_revision" {
			found = true
		}
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil {
		return rowErr
	}
	if closeErr != nil || found {
		return closeErr
	}
	_, err = db.Exec(`ALTER TABLE workspace_configs ADD COLUMN authorization_revision INTEGER NOT NULL DEFAULT 0`)
	return err
}

func ValidateRegisteredWorkspace(path string) (string, error) {
	return validateRegisteredWorkspace(path)
}

func newWorkspaceID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "ws_" + hex.EncodeToString(value), nil
}
