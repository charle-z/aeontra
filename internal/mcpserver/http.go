package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxHTTPBody caps a single MCP request body. MCP messages are small; this bounds
// memory and abuse over the network transport.
const maxHTTPBody = 4 << 20 // 4 MiB

// DefaultMCPPath is the endpoint that speaks MCP (streamable-HTTP subset).
const DefaultMCPPath = "/mcp"

// HTTPHandler returns an http.Handler that exposes the MCP tools over a
// streamable-HTTP subset (single endpoint, JSON-RPC over POST). Authentication is
// MANDATORY: every /mcp request must carry "Authorization: Bearer <token>" matching
// token, else 401. The same Server.handle path (and therefore the same Policy,
// Service, and redaction) is reused — no tool or policy logic is duplicated here.
//
// All request bodies are treated as untrusted input. Tool results still carry repo
// file contents as DATA, never instructions.
func (s *Server) HTTPHandler(token string) http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated liveness probe — no sensitive information.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	mux.HandleFunc(DefaultMCPPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !authOK(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-devbox"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			s.handleHTTPPost(w, r)
		default:
			// We do not offer a server-initiated SSE stream in L1; the streamable-HTTP
			// spec permits returning 405 for GET in that case.
			w.Header().Set("Allow", "POST")
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

// handleHTTPPost reads one JSON-RPC message (or a batch array) and replies. A lone
// notification/response yields 202 Accepted with no body; a request yields 200 with
// an application/json response; a batch yields a JSON array of the non-empty replies.
func (s *Server) handleHTTPPost(w http.ResponseWriter, r *http.Request) {
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
// mandatory bearer auth, shutting down gracefully when ctx is cancelled.
func (s *Server) ServeHTTP(ctx context.Context, addr, token string) error {
	if token == "" {
		return errors.New("http transport requires a bearer token (refusing to start without auth)")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.HTTPHandler(token),
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
