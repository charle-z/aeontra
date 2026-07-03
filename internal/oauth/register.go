package oauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// registrationRequest is the subset of RFC 7591 fields we accept. Only redirect_uris is
// required; the client is always treated as a public PKCE client.
type registrationRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	ResponseType []string `json:"response_types"`
}

// handleRegister implements Dynamic Client Registration (RFC 7591). It validates the
// redirect URIs strictly, enforces a cap + rate limit, and returns a public client_id.
// Registration alone grants nothing: the owner passphrase at /authorize is still required.
func (p *Provider) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// RFC 7591 clients may send extra metadata fields, so decode leniently (unknown
	// fields ignored); only a hard parse error is rejected.
	var req registrationRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := dec.Decode(&req); err != nil {
		registrationError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed registration body")
		return
	}
	if len(req.RedirectURIs) == 0 {
		registrationError(w, http.StatusBadRequest, "invalid_redirect_uri", "at least one redirect_uri is required")
		return
	}
	for _, u := range req.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			registrationError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	clientID, err := p.store.registerClient(req.RedirectURIs)
	if err != nil {
		if err != errRegLimited {
			registrationError(w, http.StatusInternalServerError, "server_error", "could not store client registration")
			return
		}
		registrationError(w, http.StatusTooManyRequests, "temporarily_unavailable", "registration limit reached")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"client_name":                req.ClientName,
	})
}

// validateRedirectURI enforces the OAuth 2.1 / MCP rules: absolute URI, no fragment, no
// wildcard, and HTTPS unless the host is loopback (localhost).
func validateRedirectURI(raw string) error {
	if strings.Contains(raw, "*") {
		return errorString("redirect_uri must not contain a wildcard")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errorString("redirect_uri is not a valid URL")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return errorString("redirect_uri must not contain a fragment")
	}
	if u.Host == "" {
		return errorString("redirect_uri must include a host")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return errorString("redirect_uri must use https (only localhost may use http)")
	default:
		return errorString("redirect_uri must be an http(s) URL")
	}
}

func registrationError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
