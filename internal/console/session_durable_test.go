package console

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSessionStoreSurvivesReopenAndNeverStoresRawCookie(t *testing.T) {
	root := filepath.Join(t.TempDir(), "console")
	path := filepath.Join(root, "sessions.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := NewSessionStore(SessionConfig{
		Path: path, TTL: 8 * time.Hour, MaxSessions: 8,
		Now:  func() time.Time { return now },
		Rand: bytes.NewReader(bytes.Repeat([]byte{0x31}, sessionBytes)),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	if !store.Valid(raw) {
		t.Fatal("new session is invalid")
	}
	var digest []byte
	var created, expires int64
	var version int
	if err := store.db.QueryRow(`SELECT digest,created_at,expires_at,version FROM console_sessions`).Scan(&digest, &created, &expires, &version); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(raw))
	if !bytes.Equal(digest, expected[:]) || created != now.UnixNano() || expires != now.Add(8*time.Hour).UnixNano() || version != 1 {
		t.Fatalf("stored session metadata is invalid")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(raw)) {
		t.Fatal("raw cookie value is present in database file")
	}
	reopened, err := NewSessionStore(SessionConfig{Path: path, TTL: 8 * time.Hour, MaxSessions: 8, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Valid(raw) {
		t.Fatal("session did not survive reopen")
	}
}

func TestSessionRevocationAndExpirySurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	random := append(bytes.Repeat([]byte{0x41}, sessionBytes), bytes.Repeat([]byte{0x42}, sessionBytes)...)
	store, err := NewSessionStore(SessionConfig{Path: path, TTL: time.Hour, MaxSessions: 8, Now: func() time.Time { return now }, Rand: bytes.NewReader(random)})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	expired, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	store.Revoke(revoked)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	reopened, err := NewSessionStore(SessionConfig{Path: path, TTL: time.Hour, MaxSessions: 8, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Valid(revoked) || reopened.Valid(expired) {
		t.Fatal("revoked or expired session survived as valid")
	}
	if _, ok := reopened.Expiry(revoked); ok {
		t.Fatal("revoked session exposes expiry")
	}
}

func TestSessionStoreConcurrentCreationHonorsMaximum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	var source bytes.Buffer
	for index := 1; index <= 64; index++ {
		source.Write(bytes.Repeat([]byte{byte(index)}, sessionBytes))
	}
	store, err := NewSessionStore(SessionConfig{Path: path, TTL: time.Hour, MaxSessions: 16, Now: func() time.Time { return now }, Rand: &lockedReader{reader: bytes.NewReader(source.Bytes())}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var wg sync.WaitGroup
	errorsCh := make(chan error, 32)
	for index := 0; index < 32; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create()
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count := activeSessionCount(t, store, now); count != 16 {
		t.Fatalf("active sessions=%d", count)
	}
}

type lockedReader struct {
	mu     sync.Mutex
	reader *bytes.Reader
}

func (r *lockedReader) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reader.Read(buffer)
}

func TestSessionStoreRejectsUnsafePathsPermissionsAndCorruption(t *testing.T) {
	if _, err := NewSessionStore(SessionConfig{Path: "relative/sessions.db"}); err == nil {
		t.Fatal("relative path accepted")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkDir := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Dir(target), symlinkDir); err == nil {
		if _, err := NewSessionStore(SessionConfig{Path: filepath.Join(symlinkDir, "sessions.db")}); err == nil {
			t.Fatal("symlink ancestry accepted")
		}
	}
	unsafeDir := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionStore(SessionConfig{Path: filepath.Join(unsafeDir, "sessions.db")}); err == nil {
		t.Fatal("unsafe directory permissions accepted")
	}
	corruptDir := filepath.Join(root, "corrupt")
	if err := os.Mkdir(corruptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(corruptDir, "sessions.db")
	if err := os.WriteFile(corruptPath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionStore(SessionConfig{Path: corruptPath}); err == nil {
		t.Fatal("corrupt database accepted")
	}
}

func TestSessionDatabasePermissionsAndBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	store, err := NewSessionStore(SessionConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("database mode=%v err=%v", fileInfo.Mode().Perm(), err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode=%v err=%v", dirInfo.Mode().Perm(), err)
	}
	var pageSize, maxPages int64
	if err := store.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`PRAGMA max_page_count`).Scan(&maxPages); err != nil {
		t.Fatal(err)
	}
	if maxPages != sessionDatabaseMaxPages || pageSize*maxPages > 8<<20 {
		t.Fatalf("database budget pages=%d page_size=%d", maxPages, pageSize)
	}
}
