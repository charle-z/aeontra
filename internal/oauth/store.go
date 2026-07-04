package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// tokenStore holds short-lived OAuth state in process memory. Access tokens,
// refresh tokens, and authorization codes are never persisted. Only DCR public
// client registrations may be persisted, so a ChatGPT connector can reauthorize
// after a daemon restart without deleting and recreating the connector.
type tokenStore struct {
	mu               sync.Mutex
	access           map[string]accessGrant
	codes            map[string]authCode
	refresh          map[string]refreshGrant
	clients          map[string]clientReg
	clientStorePath  string
	refreshStorePath string
	regTimes         []time.Time // sliding window of recent registration timestamps
	failTimes        []time.Time // sliding window of recent passphrase failures
}

func newTokenStore() *tokenStore {
	return &tokenStore{
		access:  make(map[string]accessGrant),
		codes:   make(map[string]authCode),
		refresh: make(map[string]refreshGrant),
		clients: make(map[string]clientReg),
	}
}

type clientStoreFile struct {
	Version int                 `json:"version"`
	Clients []clientStoreRecord `json:"clients"`
}

type clientStoreRecord struct {
	ID           string    `json:"id"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *tokenStore) enableClientPersistence(path string) error {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientStorePath = path
	return s.loadClientsLocked()
}

func (s *tokenStore) loadClientsLocked() error {
	body, err := os.ReadFile(s.clientStorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var doc clientStoreFile
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode %s: %w", s.clientStorePath, err)
	}
	if doc.Version != 1 {
		return fmt.Errorf("unsupported client store version %d", doc.Version)
	}
	if len(doc.Clients) > maxClients {
		return fmt.Errorf("client store has %d clients, max %d", len(doc.Clients), maxClients)
	}
	loaded := make(map[string]clientReg, len(doc.Clients))
	for _, c := range doc.Clients {
		if c.ID == "" {
			return fmt.Errorf("client store contains an empty client id")
		}
		if len(c.RedirectURIs) == 0 {
			return fmt.Errorf("client %s has no redirect URIs", c.ID)
		}
		for _, redirect := range c.RedirectURIs {
			if err := validateRedirectURI(redirect); err != nil {
				return fmt.Errorf("client %s redirect_uri: %w", c.ID, err)
			}
		}
		if c.CreatedAt.IsZero() {
			return fmt.Errorf("client %s has an empty created_at", c.ID)
		}
		if _, exists := loaded[c.ID]; exists {
			return fmt.Errorf("client store contains duplicate client id %s", c.ID)
		}
		loaded[c.ID] = clientReg{redirectURIs: append([]string(nil), c.RedirectURIs...), createdAt: c.CreatedAt}
	}
	s.clients = loaded
	return nil
}

func (s *tokenStore) persistClientsLocked() error {
	if s.clientStorePath == "" {
		return nil
	}
	clients := make([]clientStoreRecord, 0, len(s.clients))
	for id, c := range s.clients {
		clients = append(clients, clientStoreRecord{
			ID:           id,
			RedirectURIs: append([]string(nil), c.redirectURIs...),
			CreatedAt:    c.createdAt,
		})
	}
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ID < clients[j].ID
	})
	body, err := json.MarshalIndent(clientStoreFile{Version: 1, Clients: clients}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.clientStorePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".oauth-clients-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.clientStorePath); err != nil {
		return err
	}
	cleanup = false
	return nil
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
	if err := s.persistClientsLocked(); err != nil {
		delete(s.clients, id)
		return "", err
	}
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

func (s *tokenStore) putRefresh(token string, g refreshGrant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh[token] = g
	// Persistence is best-effort: a write error (e.g. full disk) must not break token
	// issuance. The grant stays in memory for this process's lifetime regardless.
	_ = s.persistRefreshLocked()
}

// consumeRefresh atomically returns and deletes a refresh token (rotation: a refresh
// token is valid at most once). Expired tokens are rejected.
func (s *tokenStore) consumeRefresh(token string) (refreshGrant, bool) {
	if token == "" {
		return refreshGrant{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.refresh[token]
	if !ok {
		return refreshGrant{}, false
	}
	delete(s.refresh, token)
	_ = s.persistRefreshLocked() // rotation must be durable; best-effort on write error
	if time.Now().After(g.expiresAt) {
		return refreshGrant{}, false
	}
	return g, true
}

type refreshStoreFile struct {
	Version int                  `json:"version"`
	Refresh []refreshStoreRecord `json:"refresh"`
}

type refreshStoreRecord struct {
	Token     string    `json:"token"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	Resource  string    `json:"resource"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *tokenStore) enableRefreshPersistence(path string) error {
	path = filepath.Clean(path)
	if path == "." || path == "" {
		return fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshStorePath = path
	return s.loadRefreshLocked()
}

// loadRefreshLocked reads persisted refresh tokens, dropping any that have expired.
func (s *tokenStore) loadRefreshLocked() error {
	body, err := os.ReadFile(s.refreshStorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var doc refreshStoreFile
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("decode %s: %w", s.refreshStorePath, err)
	}
	if doc.Version != 1 {
		return fmt.Errorf("unsupported refresh store version %d", doc.Version)
	}
	now := time.Now()
	loaded := make(map[string]refreshGrant, len(doc.Refresh))
	for _, r := range doc.Refresh {
		if r.Token == "" || now.After(r.ExpiresAt) {
			continue // skip empty or already-expired tokens
		}
		loaded[r.Token] = refreshGrant{
			clientID:  r.ClientID,
			scope:     r.Scope,
			resource:  r.Resource,
			expiresAt: r.ExpiresAt,
		}
	}
	s.refresh = loaded
	return nil
}

// persistRefreshLocked atomically writes the current refresh tokens (0600). Called under
// s.mu after every mutation so a restart restores exactly the live set.
func (s *tokenStore) persistRefreshLocked() error {
	if s.refreshStorePath == "" {
		return nil
	}
	records := make([]refreshStoreRecord, 0, len(s.refresh))
	for tok, g := range s.refresh {
		records = append(records, refreshStoreRecord{
			Token:     tok,
			ClientID:  g.clientID,
			Scope:     g.scope,
			Resource:  g.resource,
			ExpiresAt: g.expiresAt,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Token < records[j].Token })
	body, err := json.MarshalIndent(refreshStoreFile{Version: 1, Refresh: records}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic0600(s.refreshStorePath, body)
}

// writeFileAtomic0600 writes body to path via a temp file + rename, with 0600 perms.
func writeFileAtomic0600(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".oauth-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
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
