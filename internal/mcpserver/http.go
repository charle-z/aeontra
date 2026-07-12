package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/oauth"
)

// maxHTTPBody caps a single MCP request body. MCP messages are small; this bounds
// memory and abuse over the network transport.
const maxHTTPBody = 4 << 20 // 4 MiB

// DefaultMCPPath is the endpoint that speaks MCP (streamable-HTTP subset).
const DefaultMCPPath = "/mcp"

// HTTPHandler returns an http.Handler that exposes the MCP tools over a
// streamable-HTTP subset (single endpoint, JSON-RPC over POST). Authentication is
// MANDATORY: every /mcp request must present a valid credential, else 401.
//
// Two auth modes coexist during the transition. A static bearer/`?key=` token (legacy)
// is accepted when token != "". When oauthProvider != nil, its discovery + flow
// endpoints are mounted and a valid OAuth access token is accepted; a 401 then carries a
// WWW-Authenticate header pointing at the Protected Resource Metadata so clients such as
// ChatGPT can bootstrap the OAuth flow. The same Server.handle path (and therefore the
// same Policy, Service, and redaction) is reused — no tool or policy logic is duplicated.
//
// All request bodies are treated as untrusted input. Tool results still carry repo
// file contents as DATA, never instructions.
func (s *Server) HTTPHandler(token string, oauthProvider *oauth.Provider) http.Handler {
	mux := http.NewServeMux()
	sessionID := newHTTPSessionID()

	if oauthProvider != nil {
		oauthProvider.RegisterRoutes(mux)
	}

	// authorized accepts either the legacy static token (only when configured) or a
	// valid OAuth access token (only when OAuth is enabled).
	authorized := func(r *http.Request) bool {
		if token != "" && authOK(r, token) {
			return true
		}
		return oauthProvider != nil && oauthProvider.Authorize(r)
	}
	// challenge sets the correct WWW-Authenticate header for a 401. When OAuth is on it
	// points at the exact Protected Resource Metadata (RFC 9728 §5.1).
	challenge := func(w http.ResponseWriter) {
		if oauthProvider != nil {
			w.Header().Set("WWW-Authenticate", oauthProvider.ChallengeHeader())
		} else {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-devbox"`)
		}
	}

	// Unauthenticated liveness probe. It reports the running version + git commit so a
	// deploy can be confirmed to have shipped the latest code (no sensitive information).
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok mcp-devbox "+buildinfo.Version+" "+buildinfo.Commit+"\n")
	})

	mux.HandleFunc(DefaultMCPPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !authorized(r) {
				challenge(w)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			s.handleHTTPPost(w, r, sessionID)
		case http.MethodGet:
			if !authorized(r) {
				challenge(w)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			handleHTTPGetSSE(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return mux
}

// authOK authorizes a request by matching the configured token against either the
// "Authorization: Bearer <token>" header (preferred; used by curl, MCP Inspector,
// Claude/Cursor) OR a "?key=<token>" query parameter. The query form exists because
// ChatGPT's connector UI cannot send a custom header — the secret then rides in the
// URL (weaker: may appear in logs; use read-only + a long token, or front with
// Cloudflare Access OAuth). An empty configured token is "deny all" (fail closed).
func authOK(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) {
		got := strings.TrimSpace(h[len(prefix):])
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			return true
		}
	}
	if k := r.URL.Query().Get("key"); k != "" {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func newHTTPSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("mcp-devbox-%d", time.Now().UnixNano())
}

// handleHTTPGetSSE serves the server->client SSE stream. It stays open, sending a
// periodic keep-alive comment, until the client disconnects (request context
// cancelled). A persistent stream (vs an immediate close) avoids clients such as
// ChatGPT reconnecting in a loop. We push no server-initiated MCP messages in L1, so
// the stream carries only keep-alive comments.
func handleHTTPGetSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	writeChunk := func(s string) bool {
		if _, err := io.WriteString(w, s); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}

	if !writeChunk(": mcp-devbox stream open\n\n") {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !writeChunk(": ping\n\n") {
				return
			}
		}
	}
}

// handleHTTPPost reads one JSON-RPC message (or a batch array) and replies. A lone
// notification/response yields 202 Accepted with no body; a request yields 200 with
// an application/json response; a batch yields a JSON array of the non-empty replies.
func (s *Server) handleHTTPPost(w http.ResponseWriter, r *http.Request, sessionID string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPBody))
	if err != nil {
		// MaxBytesReader signals oversize via its error; report 413.
		var mbErr *http.MaxBytesError
		if errors.As(err, &mbErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}
	if containsInitialize([]byte(trimmed)) {
		w.Header().Set("Mcp-Session-Id", sessionID)
	}

	// JSON-RPC batch (array) support.
	if trimmed[0] == '[' {
		responses := s.handleBatch([]byte(trimmed))
		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("["))
		for i, resp := range responses {
			if i > 0 {
				w.Write([]byte(","))
			}
			w.Write(resp)
		}
		w.Write([]byte("]"))
		return
	}

	resp := s.handle([]byte(trimmed))
	if resp == nil {
		// Notification: nothing to return.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

func containsInitialize(raw []byte) bool {
	var msg struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(raw, &msg) == nil && msg.Method == "initialize" {
		return true
	}
	var batch []struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(raw, &batch) == nil {
		for _, msg := range batch {
			if msg.Method == "initialize" {
				return true
			}
		}
	}
	return false
}

// handleBatch processes each element of a JSON-RPC batch, dropping notification
// (nil) replies, and returns the raw response messages.
func (s *Server) handleBatch(raw []byte) [][]byte {
	var msgs []json.RawMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return [][]byte{mustMarshal(errorResponse(nil, -32700, "parse error"))}
	}
	var out [][]byte
	for _, m := range msgs {
		if resp := s.handle(m); resp != nil {
			out = append(out, resp)
		}
	}
	return out
}

// ServeHTTP starts an HTTP server bound to addr that serves the MCP endpoint with
// mandatory auth, shutting down gracefully when ctx is cancelled. At least one auth
// mechanism must be configured: it refuses to start only when there is neither a static
// token NOR an OAuth provider (fail closed — never serve MCP unauthenticated).
func (s *Server) ServeHTTP(ctx context.Context, addr, token string, oauthProvider *oauth.Provider) error {
	if token == "" && oauthProvider == nil {
		return errors.New("http transport requires auth: set a bearer token or enable OAuth (refusing to start without auth)")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.HTTPHandler(token, oauthProvider),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	}
}
