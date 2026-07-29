package mcpserver

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultHTTPSessionTTL = 24 * time.Hour
	maxHTTPSessions       = 4096
)

type httpSessionValidation uint8

const (
	httpSessionValid httpSessionValidation = iota
	httpSessionMissing
	httpSessionUnknown
	httpSessionExpired
)

type httpSession struct {
	createdAt time.Time
	lastSeen  time.Time
	expiresAt time.Time
}

// httpSessionStore is intentionally process-local. Session identifiers are opaque,
// never persisted, and therefore cannot carry authority across a replacement.
type httpSessionStore struct {
	mu       sync.Mutex
	sessions map[string]httpSession
	ttl      time.Duration
	now      func() time.Time
}

func newHTTPSessionStore(ttl time.Duration) *httpSessionStore {
	return newHTTPSessionStoreWithClock(ttl, time.Now)
}

func newHTTPSessionStoreWithClock(ttl time.Duration, now func() time.Time) *httpSessionStore {
	if ttl <= 0 {
		ttl = defaultHTTPSessionTTL
	}
	if now == nil {
		now = time.Now
	}
	return &httpSessionStore{
		sessions: make(map[string]httpSession),
		ttl:      ttl,
		now:      now,
	}
}

func (s *httpSessionStore) Create() string {
	if s == nil {
		return ""
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	if len(s.sessions) >= maxHTTPSessions {
		s.evictOldestLocked()
	}
	for {
		id := newHTTPSessionID()
		if _, exists := s.sessions[id]; exists {
			continue
		}
		s.sessions[id] = httpSession{
			createdAt: now,
			lastSeen:  now,
			expiresAt: now.Add(s.ttl),
		}
		return id
	}
}

func (s *httpSessionStore) Validate(id string) httpSessionValidation {
	id = strings.TrimSpace(id)
	if id == "" {
		return httpSessionMissing
	}
	if s == nil {
		return httpSessionUnknown
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, exists := s.sessions[id]
	if !exists {
		return httpSessionUnknown
	}
	if !now.Before(session.expiresAt) {
		delete(s.sessions, id)
		return httpSessionExpired
	}
	session.lastSeen = now
	session.expiresAt = now.Add(s.ttl)
	s.sessions[id] = session
	return httpSessionValid
}

func (s *httpSessionStore) Delete(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || s == nil {
		return false
	}
	s.mu.Lock()
	_, exists := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	return exists
}

func (s *httpSessionStore) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sessions = make(map[string]httpSession)
	s.mu.Unlock()
}

func (s *httpSessionStore) Count() int {
	if s == nil {
		return 0
	}
	now := s.now().UTC()
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	count := len(s.sessions)
	s.mu.Unlock()
	return count
}

func (s *httpSessionStore) pruneExpiredLocked(now time.Time) {
	for id, session := range s.sessions {
		if !now.Before(session.expiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *httpSessionStore) evictOldestLocked() {
	var oldestID string
	var oldestSeen time.Time
	for id, session := range s.sessions {
		if oldestID == "" || session.lastSeen.Before(oldestSeen) {
			oldestID = id
			oldestSeen = session.lastSeen
		}
	}
	if oldestID != "" {
		delete(s.sessions, oldestID)
	}
}

func validateHTTPSessionObserved(w http.ResponseWriter, r *http.Request, sessions *httpSessionStore, onReject func(httpSessionValidation)) (string, bool) {
	sessionID := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	validation := sessions.Validate(sessionID)
	if validation != httpSessionValid && onReject != nil {
		onReject(validation)
	}
	switch validation {
	case httpSessionValid:
		return sessionID, true
	case httpSessionMissing:
		writeHTTPSessionError(w, http.StatusBadRequest, "missing Mcp-Session-Id; call initialize")
	case httpSessionExpired:
		writeHTTPSessionError(w, http.StatusNotFound, "MCP session expired; call initialize to create a new session")
	default:
		writeHTTPSessionError(w, http.StatusNotFound, "MCP session is unknown to this server instance; call initialize to create a new session")
	}
	return "", false
}

func writeHTTPSessionError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(mustMarshal(errorResponse(nil, -32001, message)))
}
