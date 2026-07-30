package edge

import (
	"errors"
)

func (s *Store) ensureOperationLifecycleSchema() error {
	columns, err := operationColumnNames(s)
	if err != nil {
		return errors.New("edge operation schema inspection failed")
	}
	for name, statement := range map[string]string{
		"cancel_requested": `ALTER TABLE edge_operations ADD COLUMN cancel_requested INTEGER NOT NULL DEFAULT 0`,
		"progress_json":    `ALTER TABLE edge_operations ADD COLUMN progress_json BLOB`,
	} {
		if columns[name] {
			continue
		}
		if _, err := s.db.Exec(statement); err != nil {
			return errors.New("edge operation schema migration failed")
		}
	}
	return nil
}

func operationColumnNames(s *Store) (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(edge_operations)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}
