package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
)

// regTestClient registers a client directly in the store and returns its id + redirect.
func regTestClient(t *testing.T, p *Provider) (clientID, redirect string) {
	t.Helper()
	redirect = "https://chatgpt.com/connector_platform_oauth_redirect"
	id, err := p.store.registerClient([]string{redirect})
	if err != nil {
		t.Fatalf("registerClient: %v", err)
	}
	return id, redirect
}

// validAuthorizeParams returns a well-formed authorize query for the given client.
func validAuthorizeParams(p *Provider, clientID, redirect string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"}, // sample S256 challenge
		"code_challenge_method": {"S256"},
		"state":                 {"xyz-state"},
		"scope":                 {"mcp"},
		"resource":              {p.resource},
	}
}

func getAuthorize(t *testing.T, p *Provider, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pathAuthorize+"?"+q.Encode(), nil))
	return rec
}

func postAuthorize(t *testing.T, p *Provider, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, pathAuthorize, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAuthorizeGET_RendersLogin(t *testing.T) {
	p := testProvider(t)
	id, redirect := regTestClient(t, p)
	rec := getAuthorize(t, p, validAuthorizeParams(p, id, redirect))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cross-Origin-Opener-Policy"); got != authfirmware.OAuthCOOP {
		t.Fatalf("COOP = %q, want %q", got, authfirmware.OAuthCOOP)
	}
	if got, want := rec.Header().Get("Content-Security-Policy"), authorizationCSP(&authorizeParams{redirectURI: redirect}); got != want {
		t.Fatalf("CSP = %q, want %q", got, want)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="passphrase"`) {
		t.Error("login page must contain a passphrase field")
	}
	if !strings.Contains(body, `type="password"`) {
		t.Error("passphrase field must be a password input")
	}
}

func TestAuthorizeGET_RejectsBadRequests(t *testing.T) {
	p := testProvider(t)
	id, redirect := regTestClient(t, p)
	mut := func(f func(url.Values)) url.Values {
		v := validAuthorizeParams(p, id, redirect)
		f(v)
		return v
	}
	cases := map[string]url.Values{
		"unknown client":    mut(func(v url.Values) { v.Set("client_id", "does-not-exist") }),
		"redirect mismatch": mut(func(v url.Values) { v.Set("redirect_uri", "https://chatgpt.com/evil") }),
		"response not code": mut(func(v url.Values) { v.Set("response_type", "token") }),
		"pkce plain":        mut(func(v url.Values) { v.Set("code_challenge_method", "plain") }),
		"pkce missing":      mut(func(v url.Values) { v.Del("code_challenge") }),
		"resource missing":  mut(func(v url.Values) { v.Del("resource") }),
		"resource wrong":    mut(func(v url.Values) { v.Set("resource", "https://evil.example.com/mcp") }),
	}
	for name, q := range cases {
		rec := getAuthorize(t, p, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
		// A bad client/redirect must never cause an open redirect.
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s: must not redirect, got Location=%q", name, loc)
		}
	}
}

func TestAuthorizePOST_WrongPassphrase_NoCode(t *testing.T) {
	p := testProvider(t)
	id, redirect := regTestClient(t, p)
	form := validAuthorizeParams(p, id, redirect)
	form.Set("passphrase", "wrong")
	rec := postAuthorize(t, p, form)
	if rec.Code == http.StatusFound {
		t.Fatal("wrong passphrase must not redirect with a code")
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "code=") {
		t.Fatalf("wrong passphrase leaked a code: %q", loc)
	}
}

func TestAuthorizePOST_CorrectPassphrase_IssuesCode(t *testing.T) {
	p := testProvider(t)
	id, redirect := regTestClient(t, p)
	form := validAuthorizeParams(p, id, redirect)
	form.Set("passphrase", "correct horse")
	rec := postAuthorize(t, p, form)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cross-Origin-Opener-Policy"); got != authfirmware.OAuthCOOP {
		t.Fatalf("COOP = %q, want %q", got, authfirmware.OAuthCOOP)
	}
	if got, want := rec.Header().Get("Content-Security-Policy"), authorizationCSP(&authorizeParams{redirectURI: redirect}); got != want {
		t.Fatalf("CSP = %q, want %q", got, want)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := u.Query().Get("state"); got != "xyz-state" {
		t.Errorf("state = %q, want passthrough xyz-state", got)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatal("no authorization code in redirect")
	}
	// The code must be a valid single-use grant bound to this client/resource.
	if _, ok := p.store.consumeCode(code); !ok {
		t.Error("issued code should be consumable exactly once")
	}
	if _, ok := p.store.consumeCode(code); ok {
		t.Error("code must be single-use (second consume must fail)")
	}
}

func TestAuthorizePOST_RevalidatesParams(t *testing.T) {
	p := testProvider(t)
	id, _ := regTestClient(t, p)
	// Correct passphrase but a redirect_uri that is NOT registered: must be rejected
	// (POST re-validates from scratch; hidden fields are not trusted).
	form := validAuthorizeParams(p, id, "https://chatgpt.com/evil")
	form.Set("passphrase", "correct horse")
	rec := postAuthorize(t, p, form)
	if rec.Code == http.StatusFound {
		t.Fatal("tampered redirect_uri must not yield a redirect with a code")
	}
}

func TestAuthorizePOST_PassphraseThrottled(t *testing.T) {
	p := testProvider(t)
	id, redirect := regTestClient(t, p)
	form := validAuthorizeParams(p, id, redirect)
	form.Set("passphrase", "wrong")
	throttled := false
	for i := 0; i < maxPassphraseFailures+3; i++ {
		rec := postAuthorize(t, p, form)
		if rec.Code == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("repeated wrong passphrases must be throttled (429)")
	}
}

func TestAuthorizePOST_ThrottleCannotBeBypassedWithAnotherRegisteredClient(t *testing.T) {
	p := testProvider(t)
	attackerID, attackerRedirect := regTestClient(t, p)
	legitimateID, legitimateRedirect := regTestClient(t, p)
	attackerForm := validAuthorizeParams(p, attackerID, attackerRedirect)
	attackerForm.Set("passphrase", "wrong")
	for i := 0; i < maxPassphraseFailures; i++ {
		if rec := postAuthorize(t, p, attackerForm); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attacker attempt %d status = %d, want 401", i, rec.Code)
		}
	}
	if rec := postAuthorize(t, p, attackerForm); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attacker client status = %d, want 429", rec.Code)
	}

	legitimateForm := validAuthorizeParams(p, legitimateID, legitimateRedirect)
	legitimateForm.Set("passphrase", "wrong")
	if rec := postAuthorize(t, p, legitimateForm); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("another anonymous registration bypassed the global budget: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
