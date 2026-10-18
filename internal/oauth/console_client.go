package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"
)

const consoleClientScope = "mcp"

// ConsoleClient is the server-side OAuth client used by the embedded console. It is
// a public PKCE client with a deterministic identity and one exact same-origin
// callback. It contains no secret and never exposes access or refresh tokens.
type ConsoleClient struct {
	provider    *Provider
	clientID    string
	redirectURI string
}

// NewConsoleClient prepares the embedded console's own OAuth client. callbackPath
// must be an absolute same-origin path; query strings and fragments are forbidden.
func (p *Provider) NewConsoleClient(callbackPath string) (*ConsoleClient, error) {
	if p == nil {
		return nil, errors.New("oauth: provider is required")
	}
	callbackPath = strings.TrimSpace(callbackPath)
	parsed, err := url.Parse(callbackPath)
	if err != nil || !strings.HasPrefix(callbackPath, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("oauth: console callback must be an absolute path without query or fragment")
	}
	redirectURI := p.issuer + callbackPath
	if err := validateRedirectURI(redirectURI); err != nil {
		return nil, errors.New("oauth: console callback is invalid")
	}
	digest := sha256.Sum256([]byte("mcp-devbox-console-client\x00" + redirectURI))
	clientID := "console-" + base64.RawURLEncoding.EncodeToString(digest[:18])
	if err := p.store.ensureFixedClient(clientID, redirectURI); err != nil {
		return nil, err
	}
	return &ConsoleClient{provider: p, clientID: clientID, redirectURI: redirectURI}, nil
}

// AuthorizationURL returns the provider authorize endpoint for one state/PKCE pair.
// The owner passphrase is deliberately absent and is accepted only by /oauth/authorize.
func (c *ConsoleClient) AuthorizationURL(state, codeChallenge string) (string, error) {
	if c == nil || c.provider == nil || strings.TrimSpace(state) == "" || strings.TrimSpace(codeChallenge) == "" {
		return "", errors.New("oauth: console authorization state and PKCE challenge are required")
	}
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.clientID},
		"redirect_uri":          {c.redirectURI},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"scope":                 {consoleClientScope},
		"resource":              {c.provider.resource},
	}
	return c.provider.issuer + pathAuthorize + "?" + query.Encode(), nil
}

// Complete consumes and validates one authorization code against the embedded
// client's exact redirect, audience and PKCE verifier. A token pair is minted only
// inside the OAuth package and revoked immediately; no bearer reaches the console,
// browser, response, URL or logs.
func (c *ConsoleClient) Complete(codeValue, verifier string) bool {
	if c == nil || c.provider == nil || codeValue == "" || verifier == "" {
		return false
	}
	code, ok := c.provider.store.consumeCode(codeValue)
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code.clientID), []byte(c.clientID)) != 1 ||
		subtle.ConstantTimeCompare([]byte(code.redirectURI), []byte(c.redirectURI)) != 1 ||
		code.resource != c.provider.resource || code.scope != consoleClientScope ||
		!verifyPKCE(verifier, code.codeChallenge) {
		return false
	}

	access := c.provider.issueAccessToken(c.clientID, c.provider.resource, consoleClientScope, time.Minute)
	refresh := randToken()
	if access == "" || refresh == "" {
		c.provider.store.revokeAccess(access)
		return false
	}
	c.provider.store.putRefresh(refresh, refreshGrant{
		clientID:  c.clientID,
		scope:     consoleClientScope,
		resource:  c.provider.resource,
		expiresAt: time.Now().Add(time.Minute),
	})
	c.provider.store.revokeAccess(access)
	c.provider.store.revokeRefresh(refresh)
	return true
}
