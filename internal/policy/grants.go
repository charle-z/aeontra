package policy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	minGrantTTL              = time.Second
	defaultGrantTTL          = 5 * time.Minute
	maxGrantTTL              = time.Hour
	pendingRequestTTL        = 15 * time.Minute
	maxPendingAccessRequests = 256
)

var (
	ErrAccessGrantInvalid      = errors.New("policy: access grant invalid")
	ErrAccessGrantExpired      = errors.New("policy: access grant expired")
	ErrAccessGrantUsed         = errors.New("policy: access grant already used")
	ErrAccessGrantPathMismatch = errors.New("policy: access grant path mismatch")
	ErrRawAccessDenied         = errors.New("policy: raw secret access requires explicit raw grant")
	ErrAccessGrantTTL          = errors.New("policy: access grant ttl must be between 1s and 1h")
	ErrAccessRequestExpired    = errors.New("policy: access request expired")
	ErrAccessRequestLimit      = errors.New("policy: too many pending access requests")
)

// AccessRequest is created when a tool asks for a secret-named path. It is not a
// grant; it only gives the local human an opaque ID to approve out of band.
type AccessRequest struct {
	ID           string
	Path         string
	Reason       string
	RawRequested bool
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// AccessDecision records a local human decision for an access request.
type AccessDecision struct {
	RequestID string
	Path      string
	Raw       bool
	ExpiresAt time.Time
}

// AccessRequiredError is returned to MCP callers as structured JSON text.
type AccessRequiredError struct {
	Type         string `json:"type"`
	RequestID    string `json:"request_id"`
	Path         string `json:"path"`
	Reason       string `json:"reason"`
	RawRequested bool   `json:"raw_requested"`
}

func (e *AccessRequiredError) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		return "access-required"
	}
	return string(b)
}

type readGrant struct {
	path      string
	raw       bool
	expiresAt time.Time
	used      bool
}

// AccessGrants stores pending requests and approved grants in memory only. It is
// intentionally not serializable or config-backed.
type AccessGrants struct {
	mu       sync.Mutex
	requests map[string]AccessRequest
	grants   map[string]readGrant
	now      func() time.Time
	newID    func() (string, error)
	notify   func(AccessRequest)
}

func NewAccessGrants() *AccessGrants {
	return &AccessGrants{
		requests: map[string]AccessRequest{},
		grants:   map[string]readGrant{},
		now:      time.Now,
		newID:    randomID,
	}
}

func (g *AccessGrants) SetNotifier(notify func(AccessRequest)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.notify = notify
}

func (g *AccessGrants) Request(path string, raw bool) (AccessRequest, error) {
	now := g.now().UTC()
	id, err := g.newID()
	if err != nil {
		return AccessRequest{}, err
	}
	g.mu.Lock()
	g.pruneExpiredLocked(now)
	if len(g.requests) >= maxPendingAccessRequests {
		g.mu.Unlock()
		return AccessRequest{}, ErrAccessRequestLimit
	}
	req := AccessRequest{
		ID:           id,
		Path:         path,
		Reason:       "secret path read requires local human approval",
		RawRequested: raw,
		CreatedAt:    now,
		ExpiresAt:    now.Add(pendingRequestTTL),
	}
	g.requests[id] = req
	notify := g.notify
	g.mu.Unlock()
	if notify != nil {
		notify(req)
	}
	return req, nil
}

func (g *AccessGrants) Approve(id string, raw bool, ttl time.Duration) (AccessDecision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now().UTC()
	req, ok := g.requests[id]
	if !ok {
		return AccessDecision{}, ErrAccessGrantInvalid
	}
	if now.After(req.ExpiresAt) {
		delete(g.requests, id)
		return AccessDecision{}, ErrAccessRequestExpired
	}
	g.pruneExpiredLocked(now)
	if ttl == 0 {
		ttl = defaultGrantTTL
	}
	if ttl < minGrantTTL || ttl > maxGrantTTL {
		return AccessDecision{}, ErrAccessGrantTTL
	}
	decision := AccessDecision{
		RequestID: id,
		Path:      req.Path,
		Raw:       raw,
		ExpiresAt: now.Add(ttl),
	}
	g.grants[id] = readGrant{path: req.Path, raw: raw, expiresAt: decision.ExpiresAt}
	delete(g.requests, id)
	return decision, nil
}

func (g *AccessGrants) Consume(id, path string, raw bool) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.grants[id]
	if !ok {
		return false, ErrAccessGrantInvalid
	}
	if grant.used {
		return false, ErrAccessGrantUsed
	}
	if g.now().After(grant.expiresAt) {
		delete(g.grants, id)
		return false, ErrAccessGrantExpired
	}
	if grant.path != path {
		return false, ErrAccessGrantPathMismatch
	}
	if raw && !grant.raw {
		return false, ErrRawAccessDenied
	}
	grant.used = true
	g.grants[id] = grant
	return grant.raw, nil
}

func (g *AccessGrants) pruneExpiredLocked(now time.Time) {
	for id, req := range g.requests {
		if now.After(req.ExpiresAt) {
			delete(g.requests, id)
		}
	}
	for id, grant := range g.grants {
		if now.After(grant.expiresAt) {
			delete(g.grants, id)
		}
	}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
