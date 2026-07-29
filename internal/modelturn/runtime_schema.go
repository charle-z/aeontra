package modelturn

import (
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) ensureRuntimeSchema() error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS runtime_bodies (
			body_ref TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK(kind='goal'),
			content BLOB NOT NULL,
			content_digest TEXT NOT NULL,
			content_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS runtime_bodies_expiry ON runtime_bodies(expires_at)`,
		`CREATE TRIGGER IF NOT EXISTS runtime_bodies_immutable BEFORE UPDATE OF kind,content,content_digest,content_bytes,created_at,expires_at ON runtime_bodies
		BEGIN
			SELECT RAISE(ABORT, 'runtime body is immutable');
		END`,
		`CREATE TABLE IF NOT EXISTS runtime_phase_events (
			runtime_id TEXT NOT NULL,
			phase TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT '',
			count INTEGER NOT NULL,
			occurred_at INTEGER NOT NULL,
			last_at INTEGER NOT NULL,
			PRIMARY KEY(runtime_id,phase,category),
			FOREIGN KEY(runtime_id) REFERENCES model_runtimes(runtime_id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS runtime_phase_events_order ON runtime_phase_events(runtime_id,occurred_at,phase)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("model runtime schema initialization failed: %w", err)
		}
	}

	columns, err := runtimeColumns(s.db)
	if err != nil {
		return err
	}
	additions := []struct {
		name       string
		definition string
	}{
		{"device_id", `TEXT NOT NULL DEFAULT ''`},
		{"workspace_id", `TEXT NOT NULL DEFAULT ''`},
		{"controller", `TEXT NOT NULL DEFAULT 'pull_rendezvous'`},
		{"state", `TEXT NOT NULL DEFAULT 'requested'`},
		{"goal_summary", `TEXT NOT NULL DEFAULT ''`},
		{"goal_ref", `TEXT NOT NULL DEFAULT ''`},
		{"goal_digest", `TEXT NOT NULL DEFAULT ''`},
		{"expires_at", `INTEGER NOT NULL DEFAULT 0`},
		{"last_heartbeat", `INTEGER NOT NULL DEFAULT 0`},
		{"last_sequence", `INTEGER NOT NULL DEFAULT 0`},
		{"active_turn_id", `TEXT NOT NULL DEFAULT ''`},
		{"result_ref", `TEXT NOT NULL DEFAULT ''`},
		{"idempotency_key_digest", `TEXT NOT NULL DEFAULT ''`},
	}
	for _, addition := range additions {
		if columns[addition.name] {
			continue
		}
		if _, err := s.db.Exec(`ALTER TABLE model_runtimes ADD COLUMN ` + addition.name + ` ` + addition.definition); err != nil {
			return fmt.Errorf("model runtime schema migration failed: %w", err)
		}
	}

	if _, err := s.db.Exec(`UPDATE model_runtimes SET
		controller=CASE WHEN controller='' THEN 'pull_rendezvous' ELSE controller END,
		state=CASE status
			WHEN 'completed' THEN 'completed'
			WHEN 'cancelled' THEN 'cancelled'
			WHEN 'failed' THEN 'failed'
			WHEN 'running' THEN 'awaiting_model'
			ELSE CASE WHEN state='' THEN 'requested' ELSE state END
		END,
		expires_at=CASE WHEN expires_at=0 THEN updated_at+? ELSE expires_at END`, MaxTurnTTL.Nanoseconds()); err != nil {
		return errors.New("model runtime schema backfill failed")
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS model_runtimes_device_queue ON model_runtimes(device_id,controller,state,created_at)`,
		`CREATE INDEX IF NOT EXISTS model_runtimes_expiry ON model_runtimes(state,expires_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS model_runtimes_goal_ref_unique ON model_runtimes(goal_ref) WHERE goal_ref<>''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS model_runtimes_device_idempotency ON model_runtimes(device_id,idempotency_key_digest) WHERE device_id<>'' AND idempotency_key_digest<>''`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("model runtime index initialization failed: %w", err)
		}
	}
	return nil
}

func runtimeColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(model_runtimes)`)
	if err != nil {
		return nil, errors.New("model runtime schema inspection failed")
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, errors.New("model runtime schema inspection failed")
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("model runtime schema inspection failed")
	}
	return columns, nil
}
