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
	if _, err := db.Exec(`INSERT INTO edge_operations(operation_id,device_id,kind,request_json,request_digest,state,lease_id,lease_until,created_at,updated_at) VALUES('eo_0123456789abcdef0123456789abcdef','ed_0123456789abcdef0123456789abcdef','bundle_update','{"release":"stable"}','digest','leased','el_0123456789abcdef0123456789abcdef',999,123,456)`); err != nil {
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
	if err != nil || !columns["cancel_requested"] || !columns["progress_json"] ||
		!columns["lease_attempts"] || !columns["first_leased_at"] || !columns["leased_at"] ||
		!columns["running_at"] || !columns["finalizing_at"] {
		t.Fatalf("columns=%v err=%v", columns, err)
	}
	var attempts int
	var firstLeasedAt int64
	if err := store.db.QueryRow(`SELECT lease_attempts,first_leased_at FROM edge_operations WHERE operation_id='eo_0123456789abcdef0123456789abcdef'`).Scan(&attempts, &firstLeasedAt); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || firstLeasedAt != 123 {
		t.Fatalf("attempts=%d first_leased_at=%d", attempts, firstLeasedAt)
	}
}
