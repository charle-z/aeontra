package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
)

// ErrBrainNotConfigured is intentionally identical for every disabled Brain action.
// It reveals no path, configuration name, or partial capability state.
var ErrBrainNotConfigured = errors.New("brain is not configured")

// BrainReadResult combines one validated/redacted source note with bounded backlinks.
type BrainReadResult struct {
	Note      brainpkg.Note
	Backlinks []string
}

// BrainCapability owns the isolated Brain store. It shares the service audit and
// redaction core but never receives or mutates repository roots.
type BrainCapability struct {
	*serviceCore
	mu    sync.RWMutex
	store *brainpkg.Store
}

func (c *BrainCapability) configureStore(store *brainpkg.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
}

// Available reports only whether an isolated store was attached at startup.
func (c *BrainCapability) Available() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.store != nil
}

func (c *BrainCapability) acquire() (*brainpkg.Store, func(), error) {
	if c == nil {
		return nil, func() {}, ErrBrainNotConfigured
	}
	c.mu.RLock()
	if c.store == nil {
		c.mu.RUnlock()
		return nil, func() {}, ErrBrainNotConfigured
	}
	return c.store, c.mu.RUnlock, nil
}

func (c *BrainCapability) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	store := c.store
	c.store = nil
	c.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.Close()
}

func finishBrainSpan(span *audit.Span, args string, err error) {
	decision := audit.Allow
	if err != nil {
		decision = audit.Error
		if errors.Is(err, ErrBrainNotConfigured) {
			decision = audit.Deny
		}
	}
	span.Finish(decision, args, nil, err)
}

// BrainSearch performs bounded demand-driven retrieval without auditing the query.
func (c *BrainCapability) BrainSearch(ctx context.Context, query string, topK int) (results []brainpkg.SearchResult, err error) {
	span := c.log.Start("brain_search")
	defer func() { finishBrainSpan(span, fmt.Sprintf("top_k=%d", topK), err) }()
	store, release, err := c.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	results, err = store.Search(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	for index := range results {
		results[index].Title = c.redact(results[index].Title)
		results[index].Provenance = c.redact(results[index].Provenance)
		results[index].Excerpt = c.redact(results[index].Excerpt)
	}
	return results, nil
}

// BrainRead returns one redacted note and its bounded backlinks.
func (c *BrainCapability) BrainRead(ctx context.Context, slug string) (result BrainReadResult, err error) {
	span := c.log.Start("brain_read")
	defer func() { finishBrainSpan(span, "note", err) }()
	store, release, err := c.acquire()
	if err != nil {
		return BrainReadResult{}, err
	}
	defer release()
	if ctx == nil {
		return BrainReadResult{}, errors.New("brain: context is required")
	}
	note, err := store.FindBySlug(slug)
	if err != nil {
		return BrainReadResult{}, err
	}
	backlinks, err := store.Backlinks(ctx, slug)
	if err != nil {
		return BrainReadResult{}, err
	}
	note.Metadata.Title = c.redact(note.Metadata.Title)
	note.Metadata.Provenance = c.redact(note.Metadata.Provenance)
	note.Body = c.redact(note.Body)
	return BrainReadResult{Note: note, Backlinks: backlinks}, nil
}

// BrainWrite creates or updates only one working note. The draft body/provenance are
// never included in audit args or returned errors.
func (c *BrainCapability) BrainWrite(ctx context.Context, draft brainpkg.AgentDraft) (note brainpkg.Note, err error) {
	span := c.log.Start("brain_write")
	defer func() { finishBrainSpan(span, "working note", err) }()
	store, release, err := c.acquire()
	if err != nil {
		return brainpkg.Note{}, err
	}
	defer release()
	note, err = store.WriteAgent(ctx, draft)
	if err != nil {
		return brainpkg.Note{}, err
	}
	note.Metadata.Title = c.redact(note.Metadata.Title)
	note.Metadata.Provenance = c.redact(note.Metadata.Provenance)
	note.Body = c.redact(note.Body)
	return note, nil
}

// BrainIndex returns status or performs one serialized full reindex.
func (c *BrainCapability) BrainIndex(ctx context.Context, action string) (status brainpkg.IndexStatus, err error) {
	span := c.log.Start("brain_index")
	auditAction := "invalid"
	defer func() { finishBrainSpan(span, "action="+auditAction, err) }()
	store, release, err := c.acquire()
	if err != nil {
		return brainpkg.IndexStatus{}, err
	}
	defer release()
	switch action {
	case "status":
		auditAction = "status"
		return store.IndexStatus(ctx)
	case "reindex":
		auditAction = "reindex"
		return store.Reindex(ctx)
	default:
		return brainpkg.IndexStatus{}, errors.New("brain index action is invalid")
	}
}

// BrainContext returns the bounded one-line-per-note digest, never note bodies.
func (c *BrainCapability) BrainContext(ctx context.Context, limit int) (digest string, err error) {
	span := c.log.Start("brain_context")
	defer func() { finishBrainSpan(span, fmt.Sprintf("limit=%d", limit), err) }()
	store, release, err := c.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	digest, err = store.ContextDigest(ctx, limit)
	if err != nil {
		return "", err
	}
	return c.redact(digest), nil
}
