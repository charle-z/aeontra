package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/observability"
)

type transportMode string

const (
	transportStdio transportMode = "stdio"
	transportHTTP  transportMode = "http"
)

type transportConfig struct {
	Mode                 transportMode
	Addr                 string
	Token                string
	OAuth                *oauth.Provider
	AuthDescription      string
	ConsoleSecureCookies bool
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
		Mode:                 transportHTTP,
		Addr:                 normalizeHTTPAddr(opts.HTTPAddr),
		Token:                token,
		OAuth:                oauthProvider,
		AuthDescription:      authDescription,
		ConsoleSecureCookies: strings.HasPrefix(strings.ToLower(strings.TrimSpace(os.Getenv(publicURLEnv))), "https://"),
	}, nil
}

func serveTransport(runtime *appRuntime, transport transportConfig) (serveErr error) {
	transportLabel := observability.TransportStdio
	if transport.Mode == transportHTTP {
		transportLabel = observability.TransportHTTP
	}
	emitLifecycleEvent(runtime, observability.EventServerStart, transportLabel, observability.OutcomeSuccess, observability.ErrorNone)
	defer func() {
		outcome := observability.OutcomeSuccess
		errorClass := observability.ErrorNone
		if serveErr != nil {
			outcome = observability.OutcomeError
			errorClass = observability.ErrorTransport
		}
		emitLifecycleEvent(runtime, observability.EventServerStop, transportLabel, outcome, errorClass)
	}()
	if runtime.Observer == nil || !runtime.Observer.Enabled() {
		fmt.Fprintln(os.Stderr, startupDiagnostic(runtime, transport, buildinfo.Version))
	}

	if transport.Mode == transportStdio {
		return runtime.Server.Serve(os.Stdin, os.Stdout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runtime.Server.ServeHTTPWithOptions(ctx, transport.Addr, transport.Token, transport.OAuth, mcpserver.HTTPOptions{
		ConsoleSecureCookies: transport.ConsoleSecureCookies,
		EdgeHandler:          edge.NewHTTPHandler(runtime.Edge),
	})
}

func startupDiagnostic(runtime *appRuntime, transport transportConfig, version string) string {
	mode := "unknown"
	rootCount := 0
	if runtime != nil {
		if runtime.Policy != nil {
			mode = string(runtime.Policy.Mode())
			rootCount = len(runtime.Policy.Roots())
		} else if runtime.PrimaryRoot != "" {
			rootCount = 1
		}
	}
	transportName := string(transport.Mode)
	if transportName == "" {
		transportName = string(transportStdio)
	}
	return fmt.Sprintf("mcp-devbox %s serving %s (mode=%s root_count=%d)", version, transportName, mode, rootCount)
}

func emitLifecycleEvent(runtime *appRuntime, name observability.EventName, transport observability.Transport, outcome observability.Outcome, errorClass observability.ErrorClass) {
	if runtime == nil || runtime.Observer == nil {
		return
	}
	if name == observability.EventServerStop && runtime.Observer.Failures() > 0 {
		outcome = observability.OutcomeError
		errorClass = observability.ErrorInternal
	}
	event := observability.Event{
		Level:      observability.LevelInfo,
		Component:  observability.ComponentServer,
		Name:       name,
		Transport:  transport,
		Outcome:    outcome,
		ErrorClass: errorClass,
		RootCount:  1,
	}
	if outcome == observability.OutcomeError {
		event.Level = observability.LevelError
	}
	if runtime.Policy != nil {
		event.RootCount = len(runtime.Policy.Roots())
	} else if runtime.PrimaryRoot == "" {
		event.RootCount = 0
	}
	if runtime.Server != nil {
		if info, err := runtime.Server.RuntimeInfo(); err == nil {
			event.Commit = info.Commit
			event.ToolCount = info.ToolCount
			event.CatalogHash = info.CatalogHash
		}
	}
	_ = runtime.Observer.Emit(event)
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
