package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// s256 computes the PKCE code challenge for a verifier.
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func postToken(t *testing.T, p *Provider, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, pathToken, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// seedCode registers a client and stores a code bound to the given verifier's challenge.
func seedCode(t *testing.T, p *Provider, verifier string) (clientID, redirect, code string) {
	t.Helper()
	clientID, redirect = regTestClient(t, p)
	code = randToken()
	p.store.putCode(code, authCode{
		clientID:      clientID,
		redirectURI:   redirect,
		codeChallenge: s256(verifier),
		scope:         "mcp",
		resource:      p.resource,
		expiresAt:     time.Now().Add(authCodeTTL),
	})
	return
}

func decodeToken(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal token response: %v; body=%s", err, rec.Body.String())
	}
	return m
}

func TestToken_AuthorizationCode_HappyPath(t *testing.T) {
	p := testProvider(t)
	verifier := "a-sufficiently-long-random-code-verifier-1234567890"
	id, redirect, code := seedCode(t, p, verifier)

	rec := postToken(t, p, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {id},
		"code_verifier": {verifier},
		"resource":      {p.resource},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	tok := decodeToken(t, rec)
	access, _ := tok["access_token"].(string)
	if access == "" {
		t.Fatal("no access_token")
	}
	if tok["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", tok["token_type"])
	}
	if _, ok := tok["refresh_token"].(string); !ok || tok["refresh_token"] == "" {
		t.Error("no refresh_token")
	}
	// The issued access token must authorize an MCP request.
	if !p.Authorize(bearerReq(access)) {
		t.Error("issued access token must authorize")
	}
}

func TestToken_WrongVerifier_InvalidGrant(t *testing.T) {
	p := testProvider(t)
	id, redirect, code := seedCode(t, p, "the-real-verifier-xxxxxxxxxxxxxxxxxxxxxxxxx")
	rec := postToken(t, p, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {id},
		"code_verifier": {"WRONG-verifier"},
		"resource":      {p.resource},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if decodeToken(t, rec)["error"] != "invalid_grant" {
		t.Errorf("error = %v, want invalid_grant", decodeToken(t, rec)["error"])
	}
}

func TestToken_ReusedCode_Rejected(t *testing.T) {
	p := testProvider(t)
	verifier := "reuse-verifier-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	id, redirect, code := seedCode(t, p, verifier)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {id},
		"code_verifier": {verifier},
		"resource":      {p.resource},
	}
	if rec := postToken(t, p, form); rec.Code != http.StatusOK {
		t.Fatalf("first exchange should succeed, got %d", rec.Code)
	}
	if rec := postToken(t, p, form); rec.Code == http.StatusOK {
		t.Fatal("reused code must be rejected")
	}
}

func TestToken_RedirectMismatch_Rejected(t *testing.T) {
	p := testProvider(t)
	verifier := "redir-verifier-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	id, _, code := seedCode(t, p, verifier)
	rec := postToken(t, p, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://chatgpt.com/evil"},
		"client_id":     {id},
		"code_verifier": {verifier},
		"resource":      {p.resource},
	})
	if rec.Code == http.StatusOK {
		t.Fatal("redirect_uri mismatch must be rejected")
	}
}

func TestToken_ResourceMismatch_Rejected(t *testing.T) {
	p := testProvider(t)
	verifier := "res-verifier-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	id, redirect, code := seedCode(t, p, verifier)
	rec := postToken(t, p, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {id},
		"code_verifier": {verifier},
		"resource":      {"https://evil.example.com/mcp"},
	})
	if rec.Code == http.StatusOK {
		t.Fatal("resource mismatch must be rejected")
	}
}

func TestToken_UnsupportedGrant(t *testing.T) {
	p := testProvider(t)
	rec := postToken(t, p, url.Values{"grant_type": {"password"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if decodeToken(t, rec)["error"] != "unsupported_grant_type" {
		t.Errorf("error = %v, want unsupported_grant_type", decodeToken(t, rec)["error"])
	}
}

func TestToken_RefreshRotation(t *testing.T) {
	p := testProvider(t)
	verifier := "refresh-verifier-xxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	id, redirect, code := seedCode(t, p, verifier)
	first := decodeToken(t, postToken(t, p, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {id},
		"code_verifier": {verifier},
		"resource":      {p.resource},
	}))
	refresh1, _ := first["refresh_token"].(string)
	if refresh1 == "" {
		t.Fatal("no initial refresh_token")
	}

	rec := postToken(t, p, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh1},
		"client_id":     {id},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh exchange status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	second := decodeToken(t, rec)
	refresh2, _ := second["refresh_token"].(string)
	access2, _ := second["access_token"].(string)
	if refresh2 == "" || refresh2 == refresh1 {
		t.Error("refresh token must rotate to a new value")
	}
	if access2 == "" || !p.Authorize(bearerReq(access2)) {
		t.Error("refreshed access token must authorize")
	}
	// The old refresh token must be invalid after rotation.
	if rec := postToken(t, p, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh1},
		"client_id":     {id},
	}); rec.Code == http.StatusOK {
		t.Error("old refresh token must be invalidated after rotation")
	}
}

func TestToken_MethodNotAllowed(t *testing.T) {
	p := testProvider(t)
	rec := serve(t, p, http.MethodGet, pathToken)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
