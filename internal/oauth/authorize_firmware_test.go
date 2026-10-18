package oauth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/authfirmware"
)

func TestAuthorizeUsesSharedFirmwareAndStrictCSP(t *testing.T) {
	provider := testProvider(t)
	clientID, redirect := regTestClient(t, provider)
	response := getAuthorize(t, provider, validAuthorizeParams(provider, clientID, redirect))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := strings.ToLower(response.Body.String())
	if !strings.Contains(body, `href="`+strings.ToLower(authfirmware.Path)+`"`) {
		t.Fatal("shared firmware stylesheet is missing")
	}
	for _, forbidden := range []string{"<style", "style=", "<script", "onclick=", "onload=", "unsafe-inline", "authorization code", "access token", "refresh token", "code verifier"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("authorize page contains forbidden marker %q", forbidden)
		}
	}
	for _, required := range []string{"registered oauth client", "scope", "mcp", "resource", "owner passphrase", "autocomplete=\"current-password\"", "return to console", "[ ready ]"} {
		if !strings.Contains(body, required) {
			t.Fatalf("authorize page missing %q", required)
		}
	}
	wantCSP := authorizationCSP(&authorizeParams{redirectURI: redirect})
	if got := response.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("CSP=%q want=%q", got, wantCSP)
	}
}

func TestAuthorizationCSPAllowsOnlyRegisteredRedirectOrigin(t *testing.T) {
	params := &authorizeParams{redirectURI: "https://chatgpt.com/connector/oauth/callback-id?source=test"}
	got := authorizationCSP(params)
	want := "default-src 'none'; style-src 'self'; form-action 'self' https://chatgpt.com; frame-ancestors 'none'; base-uri 'none'"
	if got != want {
		t.Fatalf("CSP=%q want=%q", got, want)
	}
	for _, forbidden := range []string{"callback-id", "source=test", "*"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("CSP leaked or widened redirect detail %q: %s", forbidden, got)
		}
	}
}

func TestAuthorizationCSPFailsClosedForUnsafeRedirect(t *testing.T) {
	got := authorizationCSP(&authorizeParams{redirectURI: "https://user@chatgpt.com/callback"})
	if got != authfirmware.CSP {
		t.Fatalf("CSP=%q want fail-closed %q", got, authfirmware.CSP)
	}
}

func TestAuthorizeRejectsNonAllowlistedScope(t *testing.T) {
	provider := testProvider(t)
	clientID, redirect := regTestClient(t, provider)
	query := validAuthorizeParams(provider, clientID, redirect)
	query.Set("scope", "mcp admin")
	response := getAuthorize(t, provider, query)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func TestAuthorizeThrottleRendersLockedFirmware(t *testing.T) {
	provider := testProvider(t)
	clientID, redirect := regTestClient(t, provider)
	form := validAuthorizeParams(provider, clientID, redirect)
	form.Set("passphrase", "wrong")
	var body string
	for attempt := 0; attempt < maxPassphraseFailures+2; attempt++ {
		response := postAuthorize(t, provider, form)
		if response.Code == http.StatusTooManyRequests {
			body = strings.ToLower(response.Body.String())
			wantCSP := authorizationCSP(&authorizeParams{redirectURI: redirect})
			if got := response.Header().Get("Content-Security-Policy"); got != wantCSP {
				t.Fatalf("CSP=%q want=%q", got, wantCSP)
			}
			break
		}
	}
	if !strings.Contains(body, "[ locked ]") || !strings.Contains(body, `role="alert"`) {
		t.Fatalf("locked firmware was not rendered: %s", body)
	}
}
