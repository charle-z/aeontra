package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/grantadmin"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func serve(args []string) error {
	opts, err := parseServeOptions(args, os.Stderr)
	if err != nil {
		return err
	}
	runtime, err := buildRuntime(opts)
	if err != nil {
		return err
	}
	defer runtime.Close()
	pol := runtime.Policy
	logger := runtime.Logger
	srv := runtime.Server
	ap := runtime.AuditPath
	adminToken, err := randomHexToken()
	if err != nil {
		return fmt.Errorf("creating admin token: %w", err)
	}
	adminAddr, adminShutdown, err := grantadmin.Start("127.0.0.1:0", adminToken, pol, logger)
	if err != nil {
		return fmt.Errorf("starting local grant admin channel: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = adminShutdown(ctx)
	}()
	adminBase := "http://" + adminAddr
	pol.AccessGrants().SetNotifier(func(req policy.AccessRequest) {
		rawFlag := ""
		if req.RawRequested {
			rawFlag = " --raw --confirm-raw"
		}
		fmt.Fprintf(os.Stderr, "\nACCESS REQUIRED request_id=%s raw_requested=%t path=%s\n",
			req.ID, req.RawRequested, req.Path)
		fmt.Fprintf(os.Stderr, "Approve locally: mcp-devbox grant --admin %s --admin-token %s --ttl 5m%s %s\n\n",
			adminBase, adminToken, rawFlag, req.ID)
	})
	fmt.Fprintf(os.Stderr, "Local grant admin channel: %s (loopback only; token printed for local human approval)\n",
		adminBase)

	// HTTP transport (remote) when --http is set; stdio (local) otherwise.
	if strings.TrimSpace(opts.HTTPAddr) != "" {
		token := opts.HTTPToken
		if token == "" {
			token = os.Getenv(tokenEnv)
		}
		// Optional OAuth authorization server (enabled when both env vars are set). The
		// resource (token audience) is the canonical MCP URL = public base + /mcp.
		oauthProvider, err := buildOAuthProvider()
		if err != nil {
			return err
		}
		// Fail closed: at least one auth mechanism must be configured.
		if strings.TrimSpace(token) == "" && oauthProvider == nil {
			return fmt.Errorf("HTTP transport requires auth: set %s=<token> (or --http-token), "+
				"or enable OAuth with %s + %s. Refusing to start without auth",
				tokenEnv, publicURLEnv, oauthPassphraseEnv)
		}
		addr := normalizeHTTPAddr(opts.HTTPAddr)
		// Docker and Coolify stop containers with SIGTERM. Listening only for
		// os.Interrupt would prevent graceful draining during rolling replacement.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		authDesc := "Authorization: Bearer <token>"
		if oauthProvider != nil {
			authDesc = "OAuth (discovery via /.well-known/oauth-protected-resource)"
			if strings.TrimSpace(token) != "" {
				authDesc += " or legacy Bearer token"
			}
		}
		fmt.Fprintf(os.Stderr, "mcp-devbox %s serving HTTP on %s (mode=%s, roots=%v, audit=%s)\n",
			buildinfo.Version, addr, pol.Mode(), pol.Roots(), ap)
		fmt.Fprintf(os.Stderr, "MCP endpoint: POST http://%s%s  (%s)\n",
			addr, mcpserver.DefaultMCPPath, authDesc)
		return srv.ServeHTTP(ctx, addr, token, oauthProvider)
	}

	fmt.Fprintf(os.Stderr, "mcp-devbox %s serving stdio (mode=%s, roots=%v, audit=%s)\n",
		buildinfo.Version, pol.Mode(), pol.Roots(), ap)
	return srv.Serve(os.Stdin, os.Stdout)
}

// normalizeHTTPAddr binds host-less addresses to loopback by default. A bare port
// ("8765") or ":8765" becomes "127.0.0.1:8765"; an explicit host is preserved.
// Keeping the listener on loopback means a Cloudflare Tunnel (which connects out
// from the same host) is the only path in — no direct LAN/Internet exposure.
func normalizeHTTPAddr(a string) string {
	a = strings.TrimSpace(a)
	if !strings.Contains(a, ":") {
		a = ":" + a
	}
	if strings.HasPrefix(a, ":") {
		a = "127.0.0.1" + a
	}
	return a
}

func randomHexToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
