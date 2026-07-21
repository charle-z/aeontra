package console

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"time"
)

const (
	sessionBytes = 32
	// The production console is single-owner and should not force routine token entry.
	// Sixty years is a practical persistent horizon while still fitting in a signed
	// 32-bit cookie Max-Age value. Logout, explicit revocation, state loss, browser
	// cookie deletion, or bounded-session eviction still terminate a session.
	defaultSessionTTL   = 60 * 365 * 24 * time.Hour
	maxSessionTTL       = defaultSessionTTL
	defaultSessionCount = 128
	maxSessionCount     = 1024
)

// SessionConfig configures the bounded digest-only console session store.
type SessionConfig struct {
	Path        string
	TTL         time.Duration
	MaxSessions int
	Now         func() time.Time
	Rand        io.Reader
}

// SessionStore persists only SHA-256 digests and bounded lifecycle metadata for
// opaque browser session ids. The raw cookie value is never written to storage.
type SessionStore struct {
	db     *sql.DB
	path   string
	ttl    time.Duration
	max    int
	now    func() time.Time
	random io.Reader
}

func NewSessionStore(cfg SessionConfig) (*SessionStore, error) {
	if cfg.TTL == 0 {
		cfg.TTL = defaultSessionTTL
	}
	if cfg.MaxSessions == 0 {
		cfg.MaxSessions = defaultSessionCount
	}
	if cfg.TTL <= 0 || cfg.TTL > maxSessionTTL {
		return nil, errors.New("console session TTL is outside the allowed range")
	}
	if cfg.MaxSessions <= 0 || cfg.MaxSessions > maxSessionCount {
		return nil, errors.New("console session limit is outside the allowed range")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	db, path, err := openSessionDatabase(cfg.Path)
	if err != nil {
		return nil, err
	}
	return &SessionStore{
		db:     db,
		path:   path,
		ttl:    cfg.TTL,
		max:    cfg.MaxSessions,
		now:    cfg.Now,
		random: cfg.Rand,
	}, nil
}

func (s *SessionStore) Create() (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("console session store is unavailable")
	}
	for attempt := 0; attempt < 4; attempt++ {
		var raw [sessionBytes]byte
		if _, err := io.ReadFull(s.random, raw[:]); err != nil {
			return "", errors.New("console session id generation failed")
		}
		encoded := base64.RawURLEncoding.EncodeToString(raw[:])
		digest := sha256.Sum256([]byte(encoded))
		now := s.now().UTC()
		expires := now.Add(s.ttl)
		tx, err := s.db.Begin()
		if err != nil {
			return "", errors.New("console session transaction failed")
		}
		if _, err = tx.Exec(`DELETE FROM console_sessions WHERE expires_at<=?`, now.UnixNano()); err != nil {
			_ = tx.Rollback()
			return "", errors.New("console session pruning failed")
		}
		var active int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM console_sessions WHERE revoked_at IS NULL AND expires_at>?`, now.UnixNano()).Scan(&active); err != nil {
			_ = tx.Rollback()
			return "", errors.New("console session limit check failed")
		}
		if active >= s.max {
			if _, err = tx.Exec(`UPDATE console_sessions SET revoked_at=?,version=version+1 WHERE digest=(SELECT digest FROM console_sessions WHERE revoked_at IS NULL AND expires_at>? ORDER BY created_at,expires_at,digest LIMIT 1)`, now.UnixNano(), now.UnixNano()); err != nil {
				_ = tx.Rollback()
				return "", errors.New("console session eviction failed")
			}
			if _, err = tx.Exec(`DELETE FROM console_preferences WHERE digest IN (SELECT digest FROM console_sessions WHERE revoked_at IS NOT NULL)`); err != nil {
				_ = tx.Rollback()
				return "", errors.New("console preference eviction failed")
			}
		}
		result, err := tx.Exec(`INSERT OR IGNORE INTO console_sessions(digest,created_at,expires_at,revoked_at,version) VALUES(?,?,?,NULL,1)`, digest[:], now.UnixNano(), expires.UnixNano())
		if err != nil {
			_ = tx.Rollback()
			return "", errors.New("console session persistence failed")
		}
		rows, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return "", errors.New("console session persistence failed")
		}
		if rows != 1 {
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return "", errors.New("console session commit failed")
		}
		return encoded, nil
	}
	return "", errors.New("console session id collision limit exceeded")
}

func (s *SessionStore) Valid(raw string) bool {
	if s == nil || s.db == nil || raw == "" {
		return false
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC()
	var expires int64
	var revoked sql.NullInt64
	if err := s.db.QueryRow(`SELECT expires_at,revoked_at FROM console_sessions WHERE digest=?`, digest[:]).Scan(&expires, &revoked); err != nil {
		return false
	}
	return !revoked.Valid && expires > now.UnixNano()
}

func (s *SessionStore) Revoke(raw string) {
	if s == nil || s.db == nil || raw == "" {
		return
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC().UnixNano()
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE console_sessions SET revoked_at=?,version=version+1 WHERE digest=? AND revoked_at IS NULL`, now, digest[:]); err != nil {
		return
	}
	if _, err := tx.Exec(`DELETE FROM console_preferences WHERE digest=?`, digest[:]); err != nil {
		return
	}
	_ = tx.Commit()
}

func (s *SessionStore) Expiry(raw string) (time.Time, bool) {
	if s == nil || s.db == nil || raw == "" {
		return time.Time{}, false
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC()
	var expires int64
	var revoked sql.NullInt64
	if err := s.db.QueryRow(`SELECT expires_at,revoked_at FROM console_sessions WHERE digest=?`, digest[:]).Scan(&expires, &revoked); err != nil || revoked.Valid {
		return time.Time{}, false
	}
	expiry := time.Unix(0, expires).UTC()
	if !expiry.After(now) {
		return time.Time{}, false
	}
	return expiry, true
}

func (s *SessionStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}
