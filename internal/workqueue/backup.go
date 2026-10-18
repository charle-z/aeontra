package workqueue

import (
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func (s *Store) Backup(backupRoot string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("workqueue: store is unavailable")
	}
	backupRoot = filepath.Clean(strings.TrimSpace(backupRoot))
	if backupRoot == "." || backupRoot == "" || !filepath.IsAbs(backupRoot) || pathsOverlap(backupRoot, s.root) {
		return "", errors.New("workqueue: backup root is invalid")
	}
	if err := prepareRoot(backupRoot); err != nil {
		return "", errors.New("workqueue: backup root is unsafe")
	}
	destination := filepath.Join(backupRoot, "queue-backup.db")
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("workqueue: backup destination already exists")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		return "", errors.New("workqueue: backup checkpoint failed")
	}
	source, err := os.Open(s.path)
	if err != nil {
		return "", errors.New("workqueue: backup source unavailable")
	}
	defer source.Close()
	temporary, err := os.CreateTemp(backupRoot, ".queue-backup-*.tmp")
	if err != nil {
		return "", errors.New("workqueue: backup staging failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", errors.New("workqueue: backup permissions failed")
	}
	written, err := io.CopyN(temporary, source, TargetMaxBytes+1)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = temporary.Close()
		return "", errors.New("workqueue: backup copy failed")
	}
	if written <= 0 || written > TargetMaxBytes || temporary.Sync() != nil || temporary.Close() != nil {
		return "", errors.New("workqueue: backup bounds failed")
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		return "", errors.New("workqueue: backup activation failed")
	}
	if err := syncDirectory(backupRoot); err != nil {
		_ = os.Remove(destination)
		return "", errors.New("workqueue: backup activation failed")
	}
	if err := validateBackup(destination); err != nil {
		_ = os.Remove(destination)
		return "", err
	}
	return destination, nil
}

func RestoreBackup(backupPath string, config Config) (*Store, error) {
	backupPath = filepath.Clean(strings.TrimSpace(backupPath))
	if !filepath.IsAbs(backupPath) || validateBackup(backupPath) != nil {
		return nil, errors.New("workqueue: backup is invalid")
	}
	validated, err := validateConfig(config)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(strings.TrimSpace(validated.Root))
	if root == "." || root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("workqueue: restore root is invalid")
	}
	if err := prepareRoot(root); err != nil {
		return nil, err
	}
	if pathsOverlap(filepath.Dir(backupPath), root) {
		return nil, errors.New("workqueue: restore source overlaps destination")
	}
	destination := filepath.Join(root, "queue.db")
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("workqueue: restore destination is occupied")
	}
	source, err := os.Open(backupPath)
	if err != nil {
		return nil, errors.New("workqueue: backup unavailable")
	}
	defer source.Close()
	temporary, err := os.CreateTemp(root, ".queue-restore-*.tmp")
	if err != nil {
		return nil, errors.New("workqueue: restore staging failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return nil, errors.New("workqueue: restore permissions failed")
	}
	written, err := io.CopyN(temporary, source, TargetMaxBytes+1)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = temporary.Close()
		return nil, errors.New("workqueue: restore copy failed")
	}
	if written <= 0 || written > TargetMaxBytes || temporary.Sync() != nil || temporary.Close() != nil {
		return nil, errors.New("workqueue: restore bounds failed")
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		return nil, errors.New("workqueue: restore activation failed")
	}
	if err := syncDirectory(root); err != nil {
		removeDatabaseFiles(destination)
		return nil, errors.New("workqueue: restore activation failed")
	}
	store, err := Open(validated)
	if err != nil {
		removeDatabaseFiles(destination)
		_ = syncDirectory(root)
		return nil, err
	}
	return store, nil
}

func validateBackup(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) || info.Size() <= 0 || info.Size() > TargetMaxBytes {
		return errors.New("workqueue: backup layout is unsafe")
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return errors.New("workqueue: backup unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA query_only=ON`); err != nil {
		return errors.New("workqueue: backup unavailable")
	}
	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check(1)`).Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("workqueue: backup integrity failed")
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != schemaVersion {
		return errors.New("workqueue: backup schema mismatch")
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

func removeDatabaseFiles(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(candidate)
	}
}
