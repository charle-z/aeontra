package oauth

import (
	"net/http"
	"strings"
	"time"
)

// Authorize validates the access token on an MCP request. It reads ONLY the
// Authorization: Bearer header (access tokens must never ride the query string), looks
// the token up, and requires it to be unexpired and minted for THIS server's canonical
// resource (audience binding, RFC 8707). Returns true only when all checks pass.
func (p *Provider) Authorize(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	token := strings.TrimSpace(h[len(prefix):])
	g, ok := p.store.getAccess(token)
	if !ok {
		return false
	}
	return g.resource == p.resource
}

// ChallengeHeader is the WWW-Authenticate value returned on a 401 from the MCP endpoint.
// It points clients at this server's exact Protected Resource Metadata document for the
// /mcp resource so they can bootstrap the OAuth flow (RFC 9728 §5.1).
func (p *Provider) ChallengeHeader() string {
	return `Bearer realm="mcp-devbox", resource_metadata="` + p.issuer + pathPRMResource + `"`
}

// issueAccessToken mints and stores an opaque access token bound to a client, audience
// (resource), and scope, expiring after ttl. Used by the token endpoint (and tests).
func (p *Provider) issueAccessToken(clientID, resource, scope string, ttl time.Duration) string {
	token := randToken()
	p.store.putAccess(token, accessGrant{
		clientID:  clientID,
		resource:  resource,
		scope:     scope,
		expiresAt: time.Now().Add(ttl),
	})
	return token
}
