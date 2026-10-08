package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

// Token lifetimes. Access tokens are short-lived (a leaked token expires quickly);
// refresh tokens last longer but rotate on every use.
const (
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
)

// handleToken implements the OAuth 2.1 token endpoint for the two grant types we
// support: authorization_code (with PKCE) and refresh_token (with rotation).
func (p *Provider) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		tokenError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		p.grantAuthorizationCode(w, r)
	case "refresh_token":
		p.grantRefreshToken(w, r)
	default:
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

// grantAuthorizationCode exchanges a single-use code + PKCE verifier for tokens. Every
// binding recorded at /authorize (client, redirect, resource, PKCE) is re-checked here.
func (p *Provider) grantAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	code, ok := p.store.consumeCode(f.Get("code")) // single-use: consumed even on failure below
	if !ok {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	if subtle.ConstantTimeCompare([]byte(f.Get("client_id")), []byte(code.clientID)) != 1 {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if subtle.ConstantTimeCompare([]byte(f.Get("redirect_uri")), []byte(code.redirectURI)) != 1 {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if f.Get("resource") != code.resource {
		tokenError(w, http.StatusBadRequest, "invalid_target", "resource mismatch")
		return
	}
	if !verifyPKCE(f.Get("code_verifier"), code.codeChallenge) {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	p.issueTokenPair(w, code.clientID, code.resource, code.scope)
}

// grantRefreshToken rotates a refresh token: the presented token is consumed and a fresh
// access+refresh pair is issued (OAuth 2.1 requires rotation for public clients).
func (p *Provider) grantRefreshToken(w http.ResponseWriter, r *http.Request) {
	oldRefresh := r.PostForm.Get("refresh_token")
	access := randToken()
	refresh := randToken()
	if access == "" || refresh == "" {
		tokenError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "token generation is temporarily unavailable")
		return
	}
	now := time.Now()
	g, ok, err := p.store.rotateRefresh(oldRefresh, refresh, refreshGrant{}, access, accessGrant{}, now)
	if err != nil {
		tokenError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "token storage is temporarily unavailable")
		return
	}
	if !ok {
		tokenError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         g.scope,
	})
}

// issueTokenPair mints an access token (audience-bound to resource) and a rotating
// refresh token, and writes the token response.
func (p *Provider) issueTokenPair(w http.ResponseWriter, clientID, resource, scope string) {
	access := p.issueAccessToken(clientID, resource, scope, accessTokenTTL)
	if access == "" {
		tokenError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "access token storage is temporarily unavailable")
		return
	}
	refresh := randToken()
	if err := p.store.putRefresh(refresh, refreshGrant{
		clientID:  clientID,
		scope:     scope,
		resource:  resource,
		expiresAt: time.Now().Add(refreshTokenTTL),
	}); err != nil {
		p.store.revokeAccess(access)
		tokenError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "refresh token storage is temporarily unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

// verifyPKCE checks base64url(sha256(verifier)) == challenge in constant time (S256).
func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func tokenError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
