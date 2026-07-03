// Package oauth implements a minimal, single-owner OAuth 2.1 Authorization Server and
// Resource Server for the MCP HTTP transport, so a client such as ChatGPT can connect
// via its standard OAuth connector option instead of carrying a secret in the URL.
//
// It is deliberately small and self-contained (stdlib only, opaque in-memory tokens):
// the daemon is BOTH the resource server (validates access tokens on /mcp) and its own
// authorization server (discovery metadata, Dynamic Client Registration, an owner
// passphrase login at /authorize, and a PKCE-protected /token endpoint). It follows the
// MCP Authorization spec (2025-06-18): RFC 9728 Protected Resource Metadata, RFC 8414
// Authorization Server Metadata, RFC 7591 Dynamic Client Registration, RFC 8707 Resource
// Indicators, and OAuth 2.1 authorization-code + PKCE (S256).
//
// Security posture: OAuth is enabled only when explicitly configured; tokens are opaque,
// short-lived, single-use where applicable, never logged, and validated per request with
// strict audience (resource) binding. Optional persistence is limited to DCR public
// client registrations; tokens and authorization codes stay in process memory only.
package oauth

import (
	"fmt"
	"net/url"
	"strings"
)

// Config configures the Provider. Issuer is the public base URL of the server
// (authorization server identity); Resource is the canonical MCP endpoint URI used as
// the token audience (RFC 8707). Both must be HTTPS (localhost is exempt for dev).
type Config struct {
	Issuer          string // e.g. https://mcp-devbox-charlez.duckdns.org
	Resource        string // e.g. https://mcp-devbox-charlez.duckdns.org/mcp
	Passphrase      string // owner login secret presented at /oauth/authorize
	ClientStorePath string // optional JSON file for DCR clients only; tokens remain in memory
}

// Provider is the in-process OAuth authorization + resource server.
type Provider struct {
	issuer     string
	resource   string
	passphrase string
	store      *tokenStore
}

// NewProvider validates the config and builds a Provider. It rejects empty fields,
// non-HTTPS URLs (except localhost), and URLs carrying a fragment.
func NewProvider(cfg Config) (*Provider, error) {
	issuer := strings.TrimRight(strings.TrimSpace(cfg.Issuer), "/")
	resource := strings.TrimSpace(cfg.Resource)
	if issuer == "" {
		return nil, fmt.Errorf("oauth: issuer (public URL) is required")
	}
	if resource == "" {
		return nil, fmt.Errorf("oauth: resource (canonical MCP URL) is required")
	}
	if strings.TrimSpace(cfg.Passphrase) == "" {
		return nil, fmt.Errorf("oauth: passphrase is required")
	}
	if err := validatePublicURL(issuer); err != nil {
		return nil, fmt.Errorf("oauth: issuer: %w", err)
	}
	if err := validatePublicURL(resource); err != nil {
		return nil, fmt.Errorf("oauth: resource: %w", err)
	}
	store := newTokenStore()
	if strings.TrimSpace(cfg.ClientStorePath) != "" {
		if err := store.enableClientPersistence(cfg.ClientStorePath); err != nil {
			return nil, fmt.Errorf("oauth: client store: %w", err)
		}
	}
	return &Provider{
		issuer:     issuer,
		resource:   resource,
		passphrase: cfg.Passphrase,
		store:      store,
	}, nil
}

// validatePublicURL enforces: absolute http(s) URL, no fragment, and HTTPS unless the
// host is localhost/127.0.0.1 (development).
func validatePublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be an http(s) URL: %q", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("must not contain a fragment: %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host: %q", raw)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("must use https (only localhost may use http): %q", raw)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
