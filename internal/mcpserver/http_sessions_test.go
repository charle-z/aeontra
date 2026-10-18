package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestHTTPSessionStoreCreateTouchExpireDeleteAndReset(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store := newHTTPSessionStoreWithClock(time.Minute, func() time.Time { return now })

	if got := store.Validate(""); got != httpSessionMissing {
		t.Fatalf("missing session validation=%v", got)
	}
	if got := store.Validate("not-issued-here"); got != httpSessionUnknown {
		t.Fatalf("unknown session validation=%v", got)
	}

	first := store.Create()
	second := store.Create()
	if first == "" || second == "" || first == second {
		t.Fatalf("session identifiers are not fresh: first=%q second=%q", first, second)
	}
	if store.Count() != 2 {
		t.Fatalf("session count=%d want=2", store.Count())
	}

	now = now.Add(30 * time.Second)
	if got := store.Validate(first); got != httpSessionValid {
		t.Fatalf("valid session status=%v", got)
	}
	// Validation refreshes the idle expiry. The original one-minute deadline has
	// passed, but the refreshed session is still valid.
	now = now.Add(45 * time.Second)
	if got := store.Validate(first); got != httpSessionValid {
		t.Fatalf("touched session status=%v", got)
	}
	if got := store.Validate(second); got != httpSessionExpired {
		t.Fatalf("untouched expired session status=%v", got)
	}

	if !store.Delete(first) || store.Delete(first) {
		t.Fatal("session delete was not exact and idempotent")
	}
	third := store.Create()
	if third == "" || store.Count() != 1 {
		t.Fatalf("session recreate count=%d id=%q", store.Count(), third)
	}
	store.Reset()
	if store.Count() != 0 || store.Validate(third) != httpSessionUnknown {
		t.Fatal("session reset did not invalidate all process-local sessions")
	}
}

func TestHTTPSessionStoreIsBounded(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store := newHTTPSessionStoreWithClock(time.Hour, func() time.Time { return now })
	first := store.Create()
	for index := 1; index < maxHTTPSessions; index++ {
		now = now.Add(time.Nanosecond)
		store.Create()
	}
	if store.Count() != maxHTTPSessions {
		t.Fatalf("session count=%d want=%d", store.Count(), maxHTTPSessions)
	}
	now = now.Add(time.Nanosecond)
	store.Create()
	if store.Count() != maxHTTPSessions {
		t.Fatalf("bounded session count=%d want=%d", store.Count(), maxHTTPSessions)
	}
	if store.Validate(first) != httpSessionUnknown {
		t.Fatal("oldest idle session was not evicted at the process-local bound")
	}
}

func TestHTTPSessionValidationBlocksAuthorityFallback(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	var calls atomic.Int32
	const probe = "session_authority_probe"
	server.table[probe] = toolEntry{
		def: toolDef{
			Name:        probe,
			Description: "Test-only session authority probe.",
			InputSchema: map[string]any{"type": "object", "additionalProperties": false},
			Version:     "1",
		},
		handler: func(json.RawMessage) (string, error) {
			calls.Add(1)
			return "called", nil
		},
	}
	server.order = append(server.order, probe)

	lifecycle := newHTTPServerLifecycle()
	sessions := newHTTPSessionStore(defaultHTTPSessionTTL)
	handler := server.httpHandlerWithState(testToken, nil, HTTPOptions{}, lifecycle, sessions)
	callBody := rpcBody(t, 2, "tools/call", map[string]any{"name": probe, "arguments": map[string]any{}})

	missing := do(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, callBody)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "missing Mcp-Session-Id") {
		t.Fatalf("missing session status=%d body=%s", missing.Code, missing.Body.String())
	}
	if calls.Load() != 0 || strings.Contains(missing.Body.String(), "called") {
		t.Fatalf("missing session reached tool authority: calls=%d body=%s", calls.Load(), missing.Body.String())
	}

	unknown := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, "previous-instance-session", callBody)
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), "unknown or revoked") {
		t.Fatalf("unknown session status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	if unknown.Header().Get("Mcp-Session-Id") != "" || calls.Load() != 0 {
		t.Fatalf("unknown session recovered authority or exposed id: calls=%d headers=%v", calls.Load(), unknown.Header())
	}

	initialize := doWithHeaders(t, handler, http.MethodPost, DefaultMCPPath, rpcBody(t, 1, "initialize", nil), map[string]string{
		"Authorization":  "Bearer " + testToken,
		"Mcp-Session-Id": "previous-instance-session",
	})
	if initialize.Code != http.StatusOK {
		t.Fatalf("replacement initialize status=%d body=%s", initialize.Code, initialize.Body.String())
	}
	current := initialize.Header().Get("Mcp-Session-Id")
	if current == "" || current == "previous-instance-session" {
		t.Fatalf("initialize did not issue a fresh replacement session: %q", current)
	}

	called := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, current, callBody)
	if called.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(called.Body.String(), "called") {
		t.Fatalf("current session call status=%d calls=%d body=%s", called.Code, calls.Load(), called.Body.String())
	}

	terminated := doWithSession(t, handler, http.MethodDelete, DefaultMCPPath, "Bearer "+testToken, current, "")
	if terminated.Code != http.StatusNoContent || sessions.Count() != 0 {
		t.Fatalf("DELETE session status=%d remaining=%d", terminated.Code, sessions.Count())
	}
	revoked := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, current, callBody)
	if revoked.Code != http.StatusNotFound || calls.Load() != 1 {
		t.Fatalf("deleted session retained authority: status=%d calls=%d body=%s", revoked.Code, calls.Load(), revoked.Body.String())
	}
}

func TestHTTPExpiredSessionRequiresFreshInitialize(t *testing.T) {
	server, _ := newHTTPServerObject(t, config.ModeReadOnly)
	now := time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)
	sessions := newHTTPSessionStoreWithClock(time.Minute, func() time.Time { return now })
	handler := server.httpHandlerWithState(testToken, nil, HTTPOptions{}, newHTTPServerLifecycle(), sessions)

	oldSession := initializeHandlerSession(t, handler, "Bearer "+testToken)
	now = now.Add(2 * time.Minute)
	expired := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, oldSession, rpcBody(t, 2, "tools/list", nil))
	if expired.Code != http.StatusNotFound || !strings.Contains(expired.Body.String(), "session expired") {
		t.Fatalf("expired session status=%d body=%s", expired.Code, expired.Body.String())
	}
	if expired.Header().Get("Mcp-Session-Id") != "" {
		t.Fatalf("expired session response exposed a replacement id: %v", expired.Header())
	}

	newSession := initializeHandlerSession(t, handler, "Bearer "+testToken)
	if newSession == oldSession {
		t.Fatal("fresh initialize reused the expired session identifier")
	}
	listed := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, newSession, rpcBody(t, 3, "tools/list", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "system_runtime_info") {
		t.Fatalf("fresh session tools/list status=%d body=%s", listed.Code, listed.Body.String())
	}
}
