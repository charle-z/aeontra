package console

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const sessionDatabaseMaxPages = 1024

func openSessionDatabase(rawPath string) (*sql.DB, string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, "", errors.New("console session database is unavailable")
		}
		db.SetMaxOpenConns(1)
		if err := initializeSessionDatabase(db); err != nil {
			_ = db.Close()
			return nil, "", err
		}
		return db, "", nil
	}
	if strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) {
		return nil, "", errors.New("console session database path must be absolute")
	}
	path = filepath.Clean(path)
	if err := prepareSessionDirectory(filepath.Dir(path)); err != nil {
		return nil, "", err
	}
	newDatabase := false
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		newDatabase = true
	case err != nil:
		return nil, "", errors.New("console session database path is unavailable")
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
		return nil, "", errors.New("console session database path is unsafe")
	case info.Mode().Perm()&0o077 != 0:
		return nil, "", errors.New("console session database permissions are unsafe")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, "", errors.New("console session database is unavailable")
	}
	db.SetMaxOpenConns(1)
	if err := initializeSessionDatabase(db); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	if newDatabase {
		if err := os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, "", errors.New("console session database cannot be secured")
		}
	}
	return db, path, nil
}

func prepareSessionDirectory(directory string) error {
	clean := filepath.Clean(directory)
	if clean == "." || clean == "" || !filepath.IsAbs(clean) {
		return errors.New("console session directory must be absolute")
	}
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)
	current := volume
	if filepath.IsAbs(clean) {
		current += string(os.PathSeparator)
		remainder = strings.TrimPrefix(remainder, string(os.PathSeparator))
	}
	for _, part := range strings.Split(remainder, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return errors.New("console session directory cannot be created")
			}
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("console session directory ancestry is unsafe")
		}
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("console session directory is unsafe")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("console session directory permissions are unsafe")
	}
	return nil
}

func initializeSessionDatabase(db *sql.DB) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA secure_delete=ON`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA auto_vacuum=INCREMENTAL`,
		`PRAGMA wal_autocheckpoint=256`,
		`PRAGMA max_page_count=1024`,
		`CREATE TABLE IF NOT EXISTS console_sessions (
			digest BLOB PRIMARY KEY CHECK(length(digest)=32),
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			revoked_at INTEGER,
			version INTEGER NOT NULL CHECK(version>=1)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS console_sessions_expiry ON console_sessions(expires_at,revoked_at)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return errors.New("console session database initialization failed")
		}
	}
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil || result != "ok" {
		return errors.New("console session database integrity check failed")
	}
	return nil
}
