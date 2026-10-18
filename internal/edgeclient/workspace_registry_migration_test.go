package edgeclient

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceRegistryMigratesLegacyRowsToSandbox(t *testing.T) {
	state := t.TempDir()
	home := t.TempDir()
	devRoot := filepath.Join(home, "workspaces")
	htbRoot := filepath.Join(home, "htb-machines")
	workspace := filepath.Join(devRoot, "legacy")
	for _, path := range []string{workspace, htbRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	dbPath := filepath.Join(state, workspaceRegistryFile)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE workspaces (
		workspace_id TEXT PRIMARY KEY,
		path TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	) WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	const legacyID = "ws_0123456789abcdef0123456789abcdef"
	now := time.Now().UTC().UnixNano()
	if _, err := db.Exec(`INSERT INTO workspaces(workspace_id,path,created_at,updated_at) VALUES(?,?,?,?)`, legacyID, workspace, now, now); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := OpenWorkspaceRegistryWithRoots(state, WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	entry, err := registry.Get(legacyID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Profile != WorkspaceProfileSandbox || entry.Mode != WorkspaceModeDev || entry.Path != workspace {
		t.Fatalf("migrated entry=%+v", entry)
	}
}
