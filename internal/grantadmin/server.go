package grantadmin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/carbe/mcp-devbox/internal/audit"
	"github.com/carbe/mcp-devbox/internal/policy"
)

const DefaultPath = "/admin/grants"

// Handler exposes a local-only admin channel for human approval. It must be
// served on loopback with a daemon-generated token; MCP clients never see it.
func Handler(pol *policy.Policy, log *audit.Logger, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DefaultPath+"/", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthOK(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp-devbox-admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, DefaultPath+"/")
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPost:
			approve(w, r, pol, log, id)
		default:
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func approve(w http.ResponseWriter, r *http.Request, pol *policy.Policy, log *audit.Logger, id string) {
	var req struct {
		TTL string `json:"ttl"`
		Raw bool   `json:"raw"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ttl, err := parseTTL(req.TTL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	decision, err := pol.ApproveReadAccess(id, req.Raw, ttl)
	if err != nil {
		_ = log.Log(audit.Entry{
			Tool:     "access_grant",
			Decision: audit.Deny,
			Args:     fmt.Sprintf("approve request_id=%s raw=%t ttl=%s", id, req.Raw, req.TTL),
			Error:    err.Error(),
		})
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	_ = log.Log(audit.Entry{
		Tool:     "access_grant",
		Decision: audit.Allow,
		Args:     fmt.Sprintf("approve request_id=%s raw=%t ttl=%s", id, req.Raw, ttl),
		Files:    []string{decision.Path},
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"request_id": decision.RequestID,
		"path":       decision.Path,
		"raw":        decision.Raw,
		"expires_at": decision.ExpiresAt.Format(time.RFC3339Nano),
	})
}

func parseTTL(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	ttl, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid ttl")
	}
	if ttl < time.Second {
		return 0, fmt.Errorf("ttl must be at least 1s")
	}
	if ttl > time.Hour {
		return 0, fmt.Errorf("ttl must be <= 1h")
	}
	return ttl, nil
}

func adminAuthOK(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// Start serves Handler on addr. Use "127.0.0.1:0" to get an ephemeral local port.
func Start(addr, token string, pol *policy.Policy, log *audit.Logger) (string, func(context.Context) error, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{
		Handler:           Handler(pol, log, token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = log.Log(audit.Entry{
				Tool:     "access_grant_admin",
				Decision: audit.Error,
				Error:    err.Error(),
			})
		}
	}()
	return ln.Addr().String(), srv.Shutdown, nil
}
