package edge

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigratesLegacyOperationLifecycleSchema(t *testing.T) {
	root := filepath.Join(t.TempDir(), "edge")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "edge.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE edge_operations(operation_id TEXT PRIMARY KEY, device_id TEXT NOT NULL, kind TEXT NOT NULL, request_json BLOB NOT NULL, request_digest TEXT NOT NULL, state TEXT NOT NULL, lease_id TEXT, lease_until INTEGER, result_json BLOB, safe_code TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	columns, err := operationColumnNames(store)
	if err != nil || !columns["cancel_requested"] || !columns["progress_json"] {
		t.Fatalf("columns=%v err=%v", columns, err)
	}
}
