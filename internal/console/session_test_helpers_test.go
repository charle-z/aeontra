package console

import (
	"testing"
	"time"
)

func sessionRowCount(t *testing.T, store *SessionStore) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM console_sessions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func activeSessionCount(t *testing.T, store *SessionStore, now time.Time) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM console_sessions WHERE revoked_at IS NULL AND expires_at>?`, now.UTC().UnixNano()).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
