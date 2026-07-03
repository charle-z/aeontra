package oauth

import (
	"encoding/json"
	"net/http"
)

// Endpoint paths. The discovery documents live under /.well-known; the flow endpoints
// under /oauth. These are mounted by RegisterRoutes.
const (
	pathPRM         = "/.well-known/oauth-protected-resource"
	pathPRMResource = "/.well-known/oauth-protected-resource/mcp"
	pathASMeta      = "/.well-known/oauth-authorization-server"
	pathOIDCMeta    = "/.well-known/openid-configuration"

	pathAuthorize = "/oauth/authorize"
	pathToken     = "/oauth/token"
	pathRegister  = "/oauth/register"
)

// RegisterRoutes mounts the OAuth discovery and flow endpoints on mux. All discovery
// documents are unauthenticated GETs (clients fetch them before they hold any token).
func (p *Provider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(pathPRM, p.handleProtectedResourceMetadata)
	mux.HandleFunc(pathPRMResource, p.handleProtectedResourceMetadata)
	mux.HandleFunc(pathASMeta, p.handleAuthServerMetadata)
	mux.HandleFunc(pathOIDCMeta, p.handleAuthServerMetadata)
}

// writeJSON emits a JSON document with the correct content type. Discovery documents
// carry no secrets.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

// handleProtectedResourceMetadata serves the RFC 9728 Protected Resource Metadata,
// pointing clients at this server as its own authorization server.
func (p *Provider) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]any{
		"resource":                 p.resource,
		"authorization_servers":    []string{p.issuer},
		"scopes_supported":         []string{"mcp"},
		"bearer_methods_supported": []string{"header"},
	})
}

// handleAuthServerMetadata serves the RFC 8414 Authorization Server Metadata. It
// advertises S256-only PKCE and public clients (token_endpoint_auth_method=none).
func (p *Provider) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, map[string]any{
		"issuer":                                p.issuer,
		"authorization_endpoint":                p.issuer + pathAuthorize,
		"token_endpoint":                        p.issuer + pathToken,
		"registration_endpoint":                 p.issuer + pathRegister,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mcp"},
	})
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
