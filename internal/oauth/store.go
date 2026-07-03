package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// randToken returns a cryptographically-random, URL-safe opaque token. 32 bytes of
// entropy (256 bits) makes brute force infeasible; the value is never logged.
func randToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; if it does, fail closed with an empty string
		// (an empty token can never match a stored key).
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// accessGrant is a minted access token's server-side record. resource is the audience
// (RFC 8707): the token is only valid for the MCP server whose canonical URI matches.
type accessGrant struct {
	clientID  string
	resource  string
	scope     string
	expiresAt time.Time
}

// authCode is a single-use authorization code bound to the exact client, redirect URI,
// PKCE challenge, scope, and resource it was issued for. Consumed atomically at /token.
type authCode struct {
	clientID      string
	redirectURI   string
	codeChallenge string // PKCE S256 challenge (base64url)
	scope         string
	resource      string
	expiresAt     time.Time
}

// refreshGrant backs a refresh token. Refresh tokens rotate on every use (OAuth 2.1
// requirement for public clients): redeeming one invalidates it and mints a new pair.
type refreshGrant struct {
	clientID  string
	scope     string
	resource  string
	expiresAt time.Time
}

// tokenStore holds all short-lived OAuth state in process memory. There is no
// persistence by design: a restart drops every grant and the owner reconnects.
type tokenStore struct {
	mu      sync.Mutex
	access  map[string]accessGrant
	codes   map[string]authCode
	refresh map[string]refreshGrant
}

func newTokenStore() *tokenStore {
	return &tokenStore{
		access:  make(map[string]accessGrant),
		codes:   make(map[string]authCode),
		refresh: make(map[string]refreshGrant),
	}
}

func (s *tokenStore) putAccess(token string, g accessGrant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access[token] = g
}

// getAccess returns the grant for a token if present and unexpired. Expired grants are
// evicted on read so the map does not accumulate stale entries.
func (s *tokenStore) getAccess(token string) (accessGrant, bool) {
	if token == "" {
		return accessGrant{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.access[token]
	if !ok {
		return accessGrant{}, false
	}
	if time.Now().After(g.expiresAt) {
		delete(s.access, token)
		return accessGrant{}, false
	}
	return g, true
}
