//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeRuntimeJournalMigratesLegacyObjectiveUniqueness(t *testing.T) {
	state := t.TempDir()
	if err := os.Chmod(state, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, openCodeRuntimeJournalFile)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE local_opencode_runtimes (
			runtime_id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			goal_digest TEXT NOT NULL,
			provider_profile TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('starting','running','completed','failed','cancelled')),
			exit_code INTEGER NOT NULL DEFAULT 0,
			output_truncated INTEGER NOT NULL DEFAULT 0 CHECK(output_truncated IN (0,1)),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(workspace_id,goal_digest)
		) WITHOUT ROWID`,
		`CREATE UNIQUE INDEX local_opencode_active_workspace
		 ON local_opencode_runtimes(workspace_id) WHERE state IN ('starting','running')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	workspaceID := "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	goalDigest := "sha256:" + strings.Repeat("b", 64)
	now := time.Now().UTC().UnixNano()
	if _, err := db.Exec(
		`INSERT INTO local_opencode_runtimes(
			runtime_id,workspace_id,goal_digest,provider_profile,state,
			exit_code,output_truncated,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
		"mr_cccccccccccccccccccccccccccccccc",
		workspaceID,
		goalDigest,
		remoteProviderProfile,
		OpenCodeLocalCompleted,
		0,
		0,
		now,
		now,
	); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	journal, err := OpenOpenCodeRuntimeJournal(state)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	var schema string
	if err := journal.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='local_opencode_runtimes'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(schema), ""))
	if strings.Contains(normalized, "unique(workspace_id,goal_digest)") {
		t.Fatalf("legacy objective uniqueness remained: %s", schema)
	}
	if _, created, err := journal.Begin(
		context.Background(),
		"mr_dddddddddddddddddddddddddddddddd",
		workspaceID,
		goalDigest,
		remoteProviderProfile,
	); err != nil || !created {
		t.Fatalf("repeat objective created=%t err=%v", created, err)
	}
}
