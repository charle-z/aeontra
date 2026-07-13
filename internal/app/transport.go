package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
)

type transportMode string

const (
	transportStdio transportMode = "stdio"
	transportHTTP  transportMode = "http"
)

type transportConfig struct {
	Mode            transportMode
	Addr            string
	Token           string
	OAuth           *oauth.Provider
	AuthDescription string
}

func resolveTransport(opts serveOptions) (transportConfig, error) {
	if strings.TrimSpace(opts.HTTPAddr) == "" {
		return transportConfig{Mode: transportStdio}, nil
	}
	token := opts.HTTPToken
	if token == "" {
		token = os.Getenv(tokenEnv)
	}
	oauthProvider, err := buildOAuthProvider()
	if err != nil {
		return transportConfig{}, err
	}
	if strings.TrimSpace(token) == "" && oauthProvider == nil {
		return transportConfig{}, fmt.Errorf("HTTP transport requires auth: set %s=<token> (or --http-token), "+
			"or enable OAuth with %s + %s. Refusing to start without auth",
			tokenEnv, publicURLEnv, oauthPassphraseEnv)
	}
	authDescription := "Authorization: Bearer <token>"
	if oauthProvider != nil {
		authDescription = "OAuth (discovery via /.well-known/oauth-protected-resource)"
		if strings.TrimSpace(token) != "" {
			authDescription += " or legacy Bearer token"
		}
	}
	return transportConfig{
		Mode:            transportHTTP,
		Addr:            normalizeHTTPAddr(opts.HTTPAddr),
		Token:           token,
		OAuth:           oauthProvider,
		AuthDescription: authDescription,
	}, nil
}

func serveTransport(runtime *appRuntime, transport transportConfig) error {
	if transport.Mode == transportStdio {
		fmt.Fprintf(os.Stderr, "mcp-devbox %s serving stdio (mode=%s, roots=%v, audit=%s)\n",
			buildinfo.Version, runtime.Policy.Mode(), runtime.Policy.Roots(), runtime.AuditPath)
		return runtime.Server.Serve(os.Stdin, os.Stdout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "mcp-devbox %s serving HTTP on %s (mode=%s, roots=%v, audit=%s)\n",
		buildinfo.Version, transport.Addr, runtime.Policy.Mode(), runtime.Policy.Roots(), runtime.AuditPath)
	fmt.Fprintf(os.Stderr, "MCP endpoint: POST http://%s%s  (%s)\n",
		transport.Addr, mcpserver.DefaultMCPPath, transport.AuthDescription)
	return runtime.Server.ServeHTTP(ctx, transport.Addr, transport.Token, transport.OAuth)
}

// normalizeHTTPAddr binds host-less addresses to loopback by default. A bare port
// ("8765") or ":8765" becomes "127.0.0.1:8765"; an explicit host is preserved.
func normalizeHTTPAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return addr
}
