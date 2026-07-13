package mcpserver

import (
	"bytes"
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
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/observability"
)

// maxHTTPBody caps a single MCP request body. MCP messages are small; this bounds
// memory and abuse over the network transport.
const (
	maxHTTPBody       = 4 << 20 // 4 MiB
	maxHTTPBatchItems = 128
)

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
	return s.HTTPHandlerWithOptions(token, oauthProvider, HTTPOptions{})
}

// HTTPHandlerWithOptions preserves the existing MCP/OAuth routes and adds only the
// authenticated presentation console configured by opts.
func (s *Server) HTTPHandlerWithOptions(token string, oauthProvider *oauth.Provider, opts HTTPOptions) http.Handler {
	runtimeInfo := s.mustRuntimeInfo()
	mux := http.NewServeMux()
	sessionID := newHTTPSessionID()
	catalogNotifier := &oneShotCatalogNotifier{}

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

	consoleHandler, err := console.New(console.Config{
		StaticToken:   token,
		SecureCookies: opts.ConsoleSecureCookies,
		Runtime: console.Status{
			Status:          runtimeInfo.Status,
			Version:         runtimeInfo.Version,
			ProtocolVersion: runtimeInfo.ProtocolVersion,
			Commit:          runtimeInfo.Commit,
			ToolCount:       runtimeInfo.ToolCount,
			CatalogHash:     runtimeInfo.CatalogHash,
		},
		Authorize: authorized,
	})
	if err != nil {
		panic(fmt.Sprintf("invalid console configuration: %v", err))
	}
	consoleHandler.Register(mux)

	// Unauthenticated liveness probe. It reports the running version + git commit so a
	// deploy can be confirmed to have shipped the latest code (no sensitive information).
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok mcp-devbox "+buildinfo.Version+" "+buildinfo.Commit+"\n")
	})

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(runtimeInfo)
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
			handleHTTPGetSSE(w, r, catalogNotifier)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return s.withHTTPObservability(withRuntimeHeaders(mux, runtimeInfo))
}

func withRuntimeHeaders(next http.Handler, info RuntimeInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-MCP-Server-Commit", info.Commit)
		w.Header().Set("X-MCP-Catalog-Hash", info.CatalogHash)
		w.Header().Set("X-MCP-Tool-Count", fmt.Sprintf("%d", info.ToolCount))
		next.ServeHTTP(w, r)
	})
}

type requestIDContextKey struct{}

type observabilityResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *observabilityResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *observabilityResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *observabilityResponseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap preserves optional ResponseController interfaces of the underlying writer.
func (w *observabilityResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (s *Server) withHTTPObservability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := observability.NewRequestID()
		w.Header().Set("X-MCP-Request-ID", requestID)
		recorder := &observabilityResponseWriter{ResponseWriter: w}
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		next.ServeHTTP(recorder, r.WithContext(ctx))
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		outcome := observability.OutcomeSuccess
		level := observability.LevelInfo
		errorClass := observability.ErrorNone
		switch {
		case status == http.StatusAccepted:
			outcome = observability.OutcomeAccepted
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			outcome = observability.OutcomeDenied
		case status >= http.StatusBadRequest:
			outcome = observability.OutcomeError
			level = observability.LevelError
			errorClass = observability.ErrorTransport
		}
		if s.observer != nil {
			_ = s.observer.Emit(observability.Event{
				Level:      level,
				Component:  observability.ComponentHTTP,
				Name:       observability.EventHTTPRequest,
				RequestID:  requestID,
				Transport:  observability.TransportHTTP,
				Route:      normalizedRoute(r.URL.Path),
				Outcome:    outcome,
				StatusCode: status,
				DurationMS: time.Since(started).Milliseconds(),
				ErrorClass: errorClass,
			})
		}
	})
}

func requestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDContextKey{}).(string); ok && requestID != "" {
		return requestID
	}
	return observability.NewRequestID()
}

func normalizedRoute(path string) observability.Route {
	switch {
	case path == DefaultMCPPath:
		return observability.RouteMCP
	case path == "/healthz":
		return observability.RouteHealth
	case path == "/version":
		return observability.RouteVersion
	case path == "/console", strings.HasPrefix(path, "/console/"):
		return observability.RouteConsole
	case strings.HasPrefix(path, "/.well-known/"),
		strings.HasPrefix(path, "/oauth/"),
		path == "/authorize",
		path == "/token",
		path == "/register":
		return observability.RouteOAuth
	default:
		return observability.RouteOther
	}
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
// ChatGPT reconnecting in a loop. The stream carries keep-alive comments and one
// standards-based tool-list refresh notification after each process start.
type oneShotCatalogNotifier struct {
	mu   sync.Mutex
	sent bool
}

func (n *oneShotCatalogNotifier) Notify(write func(string) bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sent {
		return
	}
	const notification = "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n"
	if write(notification) {
		n.sent = true
	}
}

func handleHTTPGetSSE(w http.ResponseWriter, r *http.Request, notifier *oneShotCatalogNotifier) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, no-transform")
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
	notifier.Notify(writeChunk)
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
		responses := s.handleBatchObserved([]byte(trimmed), observability.TransportHTTP, requestIDFromContext(r.Context()))
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

	resp := s.handleObserved([]byte(trimmed), observability.TransportHTTP, requestIDFromContext(r.Context()))
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

// handleBatch processes one internal JSON-RPC batch.
func (s *Server) handleBatch(raw []byte) [][]byte {
	return s.handleBatchObserved(raw, observability.TransportInternal, observability.NewRequestID())
}

// handleBatchObserved processes each element of a JSON-RPC batch, dropping notification
// replies and sharing one server-generated request id across the transport operation.
func (s *Server) handleBatchObserved(raw []byte, transport observability.Transport, requestID string) [][]byte {
	started := time.Now()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		s.emitRPCFailure(transport, requestID, observability.ErrorParse, started)
		return [][]byte{mustMarshal(errorResponse(nil, -32700, "parse error"))}
	}

	out := make([][]byte, 0)
	count := 0
	for decoder.More() {
		count++
		if count > maxHTTPBatchItems {
			s.emitRPCFailure(transport, requestID, observability.ErrorInvalidParams, started)
			return [][]byte{mustMarshal(errorResponse(nil, -32600, "invalid request: batch too large"))}
		}
		var message json.RawMessage
		if err := decoder.Decode(&message); err != nil {
			s.emitRPCFailure(transport, requestID, observability.ErrorParse, started)
			return [][]byte{mustMarshal(errorResponse(nil, -32700, "parse error"))}
		}
		if response := s.handleObserved(message, transport, requestID); response != nil {
			out = append(out, response)
		}
	}
	if count == 0 {
		s.emitRPCFailure(transport, requestID, observability.ErrorInvalidParams, started)
		return [][]byte{mustMarshal(errorResponse(nil, -32600, "invalid request: empty batch"))}
	}
	if _, err := decoder.Token(); err != nil {
		s.emitRPCFailure(transport, requestID, observability.ErrorParse, started)
		return [][]byte{mustMarshal(errorResponse(nil, -32700, "parse error"))}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		s.emitRPCFailure(transport, requestID, observability.ErrorParse, started)
		return [][]byte{mustMarshal(errorResponse(nil, -32700, "parse error"))}
	}
	return out
}

// ServeHTTP starts an HTTP server bound to addr that serves the MCP endpoint with
// mandatory auth, shutting down gracefully when ctx is cancelled. At least one auth
// mechanism must be configured: it refuses to start only when there is neither a static
// token NOR an OAuth provider (fail closed — never serve MCP unauthenticated).
func (s *Server) ServeHTTP(ctx context.Context, addr, token string, oauthProvider *oauth.Provider) error {
	return s.ServeHTTPWithOptions(ctx, addr, token, oauthProvider, HTTPOptions{})
}

// ServeHTTPWithOptions starts the existing HTTP transport with additive presentation
// options. Authentication remains mandatory and the MCP wire contract is unchanged.
func (s *Server) ServeHTTPWithOptions(ctx context.Context, addr, token string, oauthProvider *oauth.Provider, opts HTTPOptions) error {
	if token == "" && oauthProvider == nil {
		return errors.New("http transport requires auth: set a bearer token or enable OAuth (refusing to start without auth)")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.HTTPHandlerWithOptions(token, oauthProvider, opts),
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
