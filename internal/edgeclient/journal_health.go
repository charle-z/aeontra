package edgeclient

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
)

type JournalHealthState string

const (
	JournalHealthEmpty             JournalHealthState = "empty"
	JournalHealthReady             JournalHealthState = "ready"
	JournalHealthPending           JournalHealthState = "pending"
	JournalHealthReconciliation    JournalHealthState = "reconciliation"
	JournalHealthMigrationRequired JournalHealthState = "migration_required"
)

type JournalHealth struct {
	State     JournalHealthState
	Entries   int
	Started   int
	Pending   int
	Delivered int
}

func InspectJournal(stateRoot string) (JournalHealth, error) {
	stateRoot = filepath.Clean(stateRoot)
	if !filepath.IsAbs(stateRoot) || stateRoot == string(os.PathSeparator) {
		return JournalHealth{}, errors.New("edge journal health root is invalid")
	}
	path := filepath.Join(stateRoot, "journal.db")
	if err := rejectSymlinkPath(path); err != nil {
		return JournalHealth{}, errors.New("edge journal health layout is unsafe")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return JournalHealth{State: JournalHealthEmpty}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) || info.Size() <= 0 || info.Size() > 32<<20 {
		return JournalHealth{}, errors.New("edge journal health layout is unsafe")
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return JournalHealth{}, errors.New("edge journal health is unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA query_only=ON`); err != nil {
		return JournalHealth{}, errors.New("edge journal health is unavailable")
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		return JournalHealth{}, errors.New("edge journal integrity check failed")
	}
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='executions'`).Scan(&tableCount); err != nil || tableCount != 1 {
		return JournalHealth{}, errors.New("edge journal schema is unavailable")
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version < 0 || version > journalSchemaVersion {
		return JournalHealth{}, errors.New("edge journal schema is unsupported")
	}
	var health JournalHealth
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END),0) FROM executions`, JournalStarted).Scan(&health.Entries, &health.Started); err != nil {
		return JournalHealth{}, errors.New("edge journal health is unavailable")
	}
	if health.Entries < 0 || health.Entries > MaxJournalEntries || health.Started < 0 || health.Started > health.Entries {
		return JournalHealth{}, errors.New("edge journal health bounds are invalid")
	}
	if health.Started > 0 {
		health.State = JournalHealthReconciliation
		return health, nil
	}
	if version < journalSchemaVersion {
		health.State = JournalHealthMigrationRequired
		return health, nil
	}
	columns, err := journalColumns(db)
	if err != nil || !columns["attempt"] || !columns["result_id"] || !columns["lease_id"] || !columns["delivered_at"] || !columns["updated_at"] {
		return JournalHealth{}, errors.New("edge journal schema is incomplete")
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN state=? AND delivered_at IS NULL THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN state=? AND delivered_at IS NOT NULL THEN 1 ELSE 0 END),0) FROM executions`, JournalCompleted, JournalCompleted).Scan(&health.Pending, &health.Delivered); err != nil {
		return JournalHealth{}, errors.New("edge journal health is unavailable")
	}
	if health.Pending < 0 || health.Delivered < 0 || health.Pending+health.Delivered != health.Entries {
		return JournalHealth{}, errors.New("edge journal health state is invalid")
	}
	if health.Pending > 0 {
		health.State = JournalHealthPending
	} else {
		health.State = JournalHealthReady
	}
	return health, nil
}
