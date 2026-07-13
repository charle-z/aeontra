package console

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	sessionBytes        = 32
	defaultSessionTTL   = 8 * time.Hour
	maxSessionTTL       = 24 * time.Hour
	defaultSessionCount = 128
	maxSessionCount     = 1024
)

// SessionConfig configures the bounded in-memory console session store.
type SessionConfig struct {
	TTL         time.Duration
	MaxSessions int
	Now         func() time.Time
	Rand        io.Reader
}

type sessionRecord struct {
	expires time.Time
}

// SessionStore stores only SHA-256 digests of opaque browser session ids.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]sessionRecord
	ttl      time.Duration
	max      int
	now      func() time.Time
	random   io.Reader
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
	return &SessionStore{
		sessions: make(map[[sha256.Size]byte]sessionRecord),
		ttl:      cfg.TTL,
		max:      cfg.MaxSessions,
		now:      cfg.Now,
		random:   cfg.Rand,
	}, nil
}

func (s *SessionStore) Create() (string, error) {
	if s == nil {
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
		s.mu.Lock()
		s.pruneLocked(now)
		if _, exists := s.sessions[digest]; exists {
			s.mu.Unlock()
			continue
		}
		if len(s.sessions) >= s.max {
			s.evictOldestLocked()
		}
		s.sessions[digest] = sessionRecord{expires: now.Add(s.ttl)}
		s.mu.Unlock()
		return encoded, nil
	}
	return "", errors.New("console session id collision limit exceeded")
}

func (s *SessionStore) Valid(raw string) bool {
	if s == nil || raw == "" {
		return false
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	record, ok := s.sessions[digest]
	return ok && record.expires.After(now)
}

func (s *SessionStore) Revoke(raw string) {
	if s == nil || raw == "" {
		return
	}
	digest := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	delete(s.sessions, digest)
	s.mu.Unlock()
}

func (s *SessionStore) Expiry(raw string) (time.Time, bool) {
	if s == nil || raw == "" {
		return time.Time{}, false
	}
	digest := sha256.Sum256([]byte(raw))
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	record, ok := s.sessions[digest]
	return record.expires, ok
}

func (s *SessionStore) pruneLocked(now time.Time) {
	for digest, record := range s.sessions {
		if !record.expires.After(now) {
			delete(s.sessions, digest)
		}
	}
}

func (s *SessionStore) evictOldestLocked() {
	var oldestDigest [sha256.Size]byte
	var oldestExpiry time.Time
	found := false
	for digest, record := range s.sessions {
		if !found || record.expires.Before(oldestExpiry) {
			oldestDigest = digest
			oldestExpiry = record.expires
			found = true
		}
	}
	if found {
		delete(s.sessions, oldestDigest)
	}
}
