package edgeclient

import "database/sql"

// ensureProjectProcessBindingColumns upgrades journals created before process
// identity captured the project claim. SQLite has no portable IF NOT EXISTS for
// ADD COLUMN, so inspect the schema first and add only missing columns. The
// defaults keep old durable process records observable and stoppable.
func ensureProjectProcessBindingColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(project_processes)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{name: "project_owner", definition: `project_owner TEXT NOT NULL DEFAULT ''`},
		{name: "project_repository", definition: `project_repository TEXT NOT NULL DEFAULT ''`},
		{name: "project_claim_generation", definition: `project_claim_generation INTEGER NOT NULL DEFAULT 0`},
		{name: "project_profile", definition: `project_profile TEXT NOT NULL DEFAULT ''`},
		{name: "project_mode", definition: `project_mode TEXT NOT NULL DEFAULT ''`},
		{name: "project_state", definition: `project_state TEXT NOT NULL DEFAULT ''`},
	} {
		name := column.name
		if columns[name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE project_processes ADD COLUMN ` + column.definition); err != nil {
			return err
		}
		columns[name] = true
	}
	return nil
}
