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

// Registration limits bound abuse of the unauthenticated DCR endpoint. A registered
// client is useless without the owner passphrase at /authorize, but we still cap total
// clients and rate-limit registrations so the endpoint cannot be used to exhaust memory.
const (
	maxClients                = 100
	maxRegistrationsPerWindow = 20
	registrationWindow        = time.Minute
)

// clientReg is a dynamically-registered public client (RFC 7591). No secret is stored:
// clients are public and authenticate the code exchange with PKCE, not a secret.
type clientReg struct {
	redirectURIs []string
	createdAt    time.Time
}

// tokenStore holds all short-lived OAuth state in process memory. There is no
// persistence by design: a restart drops every grant and the owner reconnects.
type tokenStore struct {
	mu        sync.Mutex
	access    map[string]accessGrant
	codes     map[string]authCode
	refresh   map[string]refreshGrant
	clients   map[string]clientReg
	regTimes  []time.Time // sliding window of recent registration timestamps
	failTimes []time.Time // sliding window of recent passphrase failures
}

func newTokenStore() *tokenStore {
	return &tokenStore{
		access:  make(map[string]accessGrant),
		codes:   make(map[string]authCode),
		refresh: make(map[string]refreshGrant),
		clients: make(map[string]clientReg),
	}
}

// errRegLimited is returned by registerClient when the cap or rate limit is hit.
var errRegLimited = errorString("registration limit reached")

type errorString string

func (e errorString) Error() string { return string(e) }

// registerClient enforces the cap + sliding-window rate limit, then stores a new client
// under a fresh random client_id and returns it.
func (s *tokenStore) registerClient(redirectURIs []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// Drop timestamps outside the window.
	kept := s.regTimes[:0]
	for _, t := range s.regTimes {
		if now.Sub(t) < registrationWindow {
			kept = append(kept, t)
		}
	}
	s.regTimes = kept

	if len(s.clients) >= maxClients || len(s.regTimes) >= maxRegistrationsPerWindow {
		return "", errRegLimited
	}

	id := randToken()
	s.clients[id] = clientReg{redirectURIs: redirectURIs, createdAt: now}
	s.regTimes = append(s.regTimes, now)
	return id, nil
}

func (s *tokenStore) getClient(id string) (clientReg, bool) {
	if id == "" {
		return clientReg{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[id]
	return c, ok
}

// Passphrase throttle: bound brute force of the owner login at /oauth/authorize.
const (
	maxPassphraseFailures = 5
	passphraseWindow      = time.Minute
)

// putCode stores a single-use authorization code.
func (s *tokenStore) putCode(code string, c authCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code] = c
}

// consumeCode atomically returns and deletes a code if present and unexpired. The
// delete makes codes single-use even under concurrent redemption.
func (s *tokenStore) consumeCode(code string) (authCode, bool) {
	if code == "" {
		return authCode{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[code]
	if !ok {
		return authCode{}, false
	}
	delete(s.codes, code)
	if time.Now().After(c.expiresAt) {
		return authCode{}, false
	}
	return c, true
}

// passphraseThrottled reports whether too many failed passphrase attempts occurred
// within the window (a simple global backoff; this is a single-owner server).
func (s *tokenStore) passphraseThrottled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFailuresLocked()
	return len(s.failTimes) >= maxPassphraseFailures
}

func (s *tokenStore) recordPassphraseFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFailuresLocked()
	s.failTimes = append(s.failTimes, time.Now())
}

func (s *tokenStore) resetPassphraseFailures() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failTimes = nil
}

func (s *tokenStore) pruneFailuresLocked() {
	now := time.Now()
	kept := s.failTimes[:0]
	for _, t := range s.failTimes {
		if now.Sub(t) < passphraseWindow {
			kept = append(kept, t)
		}
	}
	s.failTimes = kept
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
