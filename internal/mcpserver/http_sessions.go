package mcpserver

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultHTTPSessionTTL              = 60 * 365 * 24 * time.Hour
	defaultLegacySessionAdoptionWindow = 7 * 24 * time.Hour
	maxHTTPSessions                    = 4096
	httpSessionDatabaseFilename        = "sessions.db"
)

var legacyHTTPSessionID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type httpSessionValidation uint8

const (
	httpSessionValid httpSessionValidation = iota
	httpSessionMissing
	httpSessionUnknown
	httpSessionExpired
)

type httpSessionRecord struct {
	Capabilities       ClientCapabilities
	InitialCatalogHash string
	LastCatalogHash    string
	AdoptedLegacy      bool
}

// HTTPSessionStore is service-owned durable MCP session state. Only SHA-256
// digests of the opaque session identifier and authenticated principal are stored.
// Access tokens, refresh tokens, Authorization headers and raw session ids are not.
type HTTPSessionStore struct {
	mu            sync.Mutex
	db            *sql.DB
	ttl           time.Duration
	now           func() time.Time
	adoptionUntil time.Time
	durable       bool
}

type httpSessionStore = HTTPSessionStore

// OpenHTTPSessionStore opens the durable session database below one private state
// directory. Separate server processes may open the same database during a rolling
// replacement; WAL and bounded transactions provide the shared service boundary.
func OpenHTTPSessionStore(root string) (*HTTPSessionStore, error) {
	store, err := openHTTPSessionStoreWithClock(root, defaultHTTPSessionTTL, time.Now)
	if err != nil {
		return nil, err
	}
	store.durable = strings.TrimSpace(root) != ""
	return store, nil
}

func openHTTPSessionStoreWithClock(root string, ttl time.Duration, now func() time.Time) (*HTTPSessionStore, error) {
	if ttl <= 0 {
		ttl = defaultHTTPSessionTTL
	}
	if now == nil {
		now = time.Now
	}

	var dsn string
	var databasePath string
	root = strings.TrimSpace(root)
	if root == "" {
		dsn = "file:mcp-session-" + newHTTPSessionID() + "?mode=memory&cache=shared"
	} else {
		root = filepath.Clean(root)
		if !filepath.IsAbs(root) || strings.ContainsRune(root, '\x00') {
			return nil, errors.New("MCP session root must be absolute")
		}
		if err := prepareHTTPSessionRoot(root); err != nil {
			return nil, err
		}
		databasePath = filepath.Join(root, httpSessionDatabaseFilename)
		if info, err := os.Lstat(databasePath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, errors.New("MCP session database is unsafe")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("MCP session database is unavailable")
		}
		dsn = databasePath
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, errors.New("MCP session database is unavailable")
	}
	db.SetMaxOpenConns(1)
	store := &HTTPSessionStore{db: db, ttl: ttl, now: now, durable: databasePath != ""}
	if err := store.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if databasePath != "" {
		if err := os.Chmod(databasePath, 0o600); err != nil {
			_ = db.Close()
			return nil, errors.New("MCP session database permissions could not be secured")
		}
	}
	return store, nil
}

func newHTTPSessionStore(ttl time.Duration) *HTTPSessionStore {
	return newHTTPSessionStoreWithClock(ttl, time.Now)
}

func newHTTPSessionStoreWithClock(ttl time.Duration, now func() time.Time) *HTTPSessionStore {
	store, err := openHTTPSessionStoreWithClock("", ttl, now)
	if err != nil {
		panic(err)
	}
	return store
}

func (s *HTTPSessionStore) initialize() error {
	now := s.now().UTC()
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS mcp_session_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS mcp_sessions (
			session_digest TEXT PRIMARY KEY,
			principal_digest TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('active','revoked','expired')),
			created_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			initial_catalog_hash TEXT NOT NULL,
			last_catalog_hash TEXT NOT NULL,
			capabilities_json TEXT NOT NULL,
			adopted_legacy INTEGER NOT NULL CHECK(adopted_legacy IN (0,1))
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS mcp_sessions_active_seen ON mcp_sessions(state,last_seen)`,
		`CREATE INDEX IF NOT EXISTS mcp_sessions_expiry ON mcp_sessions(state,expires_at)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("MCP session database initialization failed: %w", err)
		}
	}

	defaultUntil := now.Add(defaultLegacySessionAdoptionWindow).UnixNano()
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO mcp_session_meta(key,value) VALUES('legacy_adoption_until',?)`, fmt.Sprintf("%d", defaultUntil)); err != nil {
		return errors.New("MCP session migration metadata could not be initialized")
	}
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM mcp_session_meta WHERE key='legacy_adoption_until'`).Scan(&raw); err != nil {
		return errors.New("MCP session migration metadata could not be read")
	}
	var unixNano int64
	if _, err := fmt.Sscan(raw, &unixNano); err != nil || unixNano <= 0 {
		return errors.New("MCP session migration metadata is invalid")
	}
	s.adoptionUntil = time.Unix(0, unixNano).UTC()
	return nil
}

func (s *HTTPSessionStore) CreateBound(principal, catalogHash string, capabilities ClientCapabilities) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("MCP session store is unavailable")
	}
	principalDigest := digestHTTPSessionValue("principal", principal)
	if principalDigest == "" {
		return "", errors.New("MCP session principal is invalid")
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return "", errors.New("MCP session capabilities are invalid")
	}
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return "", errors.New("MCP session transaction failed")
	}
	defer tx.Rollback()
	if err := expireHTTPSessions(tx, now); err != nil {
		return "", err
	}
	if err := enforceHTTPSessionBound(tx); err != nil {
		return "", err
	}
	for attempts := 0; attempts < 8; attempts++ {
		id := newHTTPSessionID()
		digest := digestHTTPSessionValue("session", id)
		_, err = tx.Exec(`INSERT INTO mcp_sessions(
			session_digest,principal_digest,state,created_at,last_seen,expires_at,
			initial_catalog_hash,last_catalog_hash,capabilities_json,adopted_legacy
		) VALUES(?,?,'active',?,?,?,?,?,?,0)`, digest, principalDigest, now.UnixNano(), now.UnixNano(), now.Add(s.ttl).UnixNano(), catalogHash, catalogHash, string(capabilitiesJSON))
		if err == nil {
			if err := tx.Commit(); err != nil {
				return "", errors.New("MCP session commit failed")
			}
			return id, nil
		}
	}
	return "", errors.New("MCP session id generation failed")
}

// ValidateBound validates and touches one session for an authenticated principal.
// During the one-time migration window, a correctly formed legacy session id that is
// absent from the durable database may be adopted by the authenticated principal.
func (s *HTTPSessionStore) ValidateBound(id, principal, catalogHash string, allowLegacyAdoption bool) (httpSessionRecord, httpSessionValidation, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return httpSessionRecord{}, httpSessionMissing, nil
	}
	if s == nil || s.db == nil {
		return httpSessionRecord{}, httpSessionUnknown, errors.New("MCP session store is unavailable")
	}
	principalDigest := digestHTTPSessionValue("principal", principal)
	if principalDigest == "" {
		return httpSessionRecord{}, httpSessionUnknown, nil
	}
	digest := digestHTTPSessionValue("session", id)
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return httpSessionRecord{}, httpSessionUnknown, errors.New("MCP session transaction failed")
	}
	defer tx.Rollback()
	record, storedPrincipal, state, expiresAt, found, err := readHTTPSession(tx, digest)
	if err != nil {
		return httpSessionRecord{}, httpSessionUnknown, err
	}
	if !found {
		if !allowLegacyAdoption || now.After(s.adoptionUntil) || !legacyHTTPSessionID.MatchString(id) {
			return httpSessionRecord{}, httpSessionUnknown, nil
		}
		capabilitiesJSON, _ := json.Marshal(ClientCapabilities{})
		_, err := tx.Exec(`INSERT OR IGNORE INTO mcp_sessions(
			session_digest,principal_digest,state,created_at,last_seen,expires_at,
			initial_catalog_hash,last_catalog_hash,capabilities_json,adopted_legacy
		) VALUES(?,?,'active',?,?,?,?,?,?,1)`, digest, principalDigest, now.UnixNano(), now.UnixNano(), now.Add(s.ttl).UnixNano(), catalogHash, catalogHash, string(capabilitiesJSON))
		if err != nil {
			return httpSessionRecord{}, httpSessionUnknown, errors.New("legacy MCP session adoption failed")
		}
		record, storedPrincipal, state, expiresAt, found, err = readHTTPSession(tx, digest)
		if err != nil || !found {
			return httpSessionRecord{}, httpSessionUnknown, errors.New("legacy MCP session adoption could not be verified")
		}
	}
	if storedPrincipal != principalDigest {
		return httpSessionRecord{}, httpSessionUnknown, nil
	}
	if state == "revoked" {
		return httpSessionRecord{}, httpSessionUnknown, nil
	}
	if state == "expired" || !now.Before(expiresAt) {
		_, _ = tx.Exec(`UPDATE mcp_sessions SET state='expired' WHERE session_digest=? AND state='active'`, digest)
		_ = tx.Commit()
		return httpSessionRecord{}, httpSessionExpired, nil
	}
	if _, err := tx.Exec(`UPDATE mcp_sessions SET last_seen=?,expires_at=?,last_catalog_hash=? WHERE session_digest=? AND state='active'`, now.UnixNano(), now.Add(s.ttl).UnixNano(), catalogHash, digest); err != nil {
		return httpSessionRecord{}, httpSessionUnknown, errors.New("MCP session touch failed")
	}
	if err := tx.Commit(); err != nil {
		return httpSessionRecord{}, httpSessionUnknown, errors.New("MCP session commit failed")
	}
	record.LastCatalogHash = catalogHash
	return record, httpSessionValid, nil
}

func (s *HTTPSessionStore) UpdateCapabilitiesBound(id, principal, catalogHash string, capabilities ClientCapabilities) error {
	if s == nil || s.db == nil {
		return errors.New("MCP session store is unavailable")
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return errors.New("MCP session capabilities are invalid")
	}
	result, err := s.db.Exec(`UPDATE mcp_sessions SET capabilities_json=?,last_catalog_hash=? WHERE session_digest=? AND principal_digest=? AND state='active'`, string(capabilitiesJSON), catalogHash, digestHTTPSessionValue("session", id), digestHTTPSessionValue("principal", principal))
	if err != nil {
		return errors.New("MCP session capabilities could not be persisted")
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return errors.New("MCP session is unavailable")
	}
	return nil
}

func (s *HTTPSessionStore) DeleteBound(id, principal string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" || s == nil || s.db == nil {
		return false, nil
	}
	result, err := s.db.Exec(`UPDATE mcp_sessions SET state='revoked' WHERE session_digest=? AND principal_digest=? AND state='active'`, digestHTTPSessionValue("session", id), digestHTTPSessionValue("principal", principal))
	if err != nil {
		return false, errors.New("MCP session revocation failed")
	}
	rows, _ := result.RowsAffected()
	return rows == 1, nil
}

func readHTTPSession(tx *sql.Tx, digest string) (httpSessionRecord, string, string, time.Time, bool, error) {
	var principalDigest, state, initialCatalogHash, lastCatalogHash, capabilitiesJSON string
	var expiresAt int64
	var adoptedLegacy int
	err := tx.QueryRow(`SELECT principal_digest,state,expires_at,initial_catalog_hash,last_catalog_hash,capabilities_json,adopted_legacy FROM mcp_sessions WHERE session_digest=?`, digest).
		Scan(&principalDigest, &state, &expiresAt, &initialCatalogHash, &lastCatalogHash, &capabilitiesJSON, &adoptedLegacy)
	if errors.Is(err, sql.ErrNoRows) {
		return httpSessionRecord{}, "", "", time.Time{}, false, nil
	}
	if err != nil {
		return httpSessionRecord{}, "", "", time.Time{}, false, errors.New("MCP session lookup failed")
	}
	var capabilities ClientCapabilities
	if err := json.Unmarshal([]byte(capabilitiesJSON), &capabilities); err != nil {
		return httpSessionRecord{}, "", "", time.Time{}, false, errors.New("MCP session capabilities are corrupt")
	}
	return httpSessionRecord{
		Capabilities:       capabilities,
		InitialCatalogHash: initialCatalogHash,
		LastCatalogHash:    lastCatalogHash,
		AdoptedLegacy:      adoptedLegacy == 1,
	}, principalDigest, state, time.Unix(0, expiresAt).UTC(), true, nil
}

func expireHTTPSessions(tx *sql.Tx, now time.Time) error {
	if _, err := tx.Exec(`UPDATE mcp_sessions SET state='expired' WHERE state='active' AND expires_at<=?`, now.UnixNano()); err != nil {
		return errors.New("MCP session expiry cleanup failed")
	}
	return nil
}

func enforceHTTPSessionBound(tx *sql.Tx) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM mcp_sessions WHERE state='active'`).Scan(&count); err != nil {
		return errors.New("MCP session count failed")
	}
	if count < maxHTTPSessions {
		return nil
	}
	if _, err := tx.Exec(`UPDATE mcp_sessions SET state='revoked' WHERE session_digest=(SELECT session_digest FROM mcp_sessions WHERE state='active' ORDER BY last_seen ASC LIMIT 1)`); err != nil {
		return errors.New("MCP session eviction failed")
	}
	return nil
}

func digestHTTPSessionValue(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte("mcp-devbox:" + kind + ":" + value))
	return hex.EncodeToString(digest[:])
}

func prepareHTTPSessionRoot(root string) error {
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("MCP session root is unsafe")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return errors.New("MCP session root could not be created")
		}
	} else {
		return errors.New("MCP session root is unavailable")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return errors.New("MCP session root permissions could not be secured")
	}
	return nil
}

// Compatibility helpers keep the existing transport wiring small. HTTP requests are
// still authenticated independently on every call; the logical owner identity is stable
// across access-token rotation and container replacement.
func (s *HTTPSessionStore) Create() string {
	id, _ := s.CreateBound("owner", "", ClientCapabilities{})
	return id
}

func (s *HTTPSessionStore) Validate(id string) httpSessionValidation {
	_, validation, _ := s.ValidateBound(id, "owner", "", false)
	return validation
}

func (s *HTTPSessionStore) Delete(id string) bool {
	deleted, _ := s.DeleteBound(id, "owner")
	return deleted
}

func (s *HTTPSessionStore) Reset() {
	if s == nil || s.db == nil || s.durable {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM mcp_sessions`)
}

func (s *HTTPSessionStore) Count() int {
	if s == nil || s.db == nil {
		return 0
	}
	_, _ = s.db.Exec(`UPDATE mcp_sessions SET state='expired' WHERE state='active' AND expires_at<=?`, s.now().UTC().UnixNano())
	var count int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM mcp_sessions WHERE state='active'`).Scan(&count)
	return count
}

func (s *HTTPSessionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func validateHTTPSessionObserved(w http.ResponseWriter, r *http.Request, sessions *HTTPSessionStore, principal, catalogHash string, allowLegacyAdoption bool, onReject func(httpSessionValidation)) (string, httpSessionRecord, bool) {
	sessionID := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	record, validation, err := sessions.ValidateBound(sessionID, principal, catalogHash, allowLegacyAdoption)
	if err != nil {
		writeHTTPSessionError(w, http.StatusServiceUnavailable, "MCP session storage is temporarily unavailable")
		return "", httpSessionRecord{}, false
	}
	if validation != httpSessionValid && onReject != nil {
		onReject(validation)
	}
	switch validation {
	case httpSessionValid:
		return sessionID, record, true
	case httpSessionMissing:
		writeHTTPSessionError(w, http.StatusBadRequest, "missing Mcp-Session-Id; call initialize")
	case httpSessionExpired:
		writeHTTPSessionError(w, http.StatusNotFound, "MCP session expired; call initialize to create a new session")
	default:
		writeHTTPSessionError(w, http.StatusNotFound, "MCP session is unknown or revoked")
	}
	return "", httpSessionRecord{}, false
}

func writeHTTPSessionError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(mustMarshal(errorResponse(nil, -32001, message)))
}
