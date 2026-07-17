package console

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func assertSecureConsoleCookie(t *testing.T, cookie *http.Cookie, deleting bool) {
	t.Helper()
	if cookie == nil || cookie.Name != cookieName {
		t.Fatalf("unexpected console cookie: %+v", cookie)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unsafe console cookie attributes: %+v", cookie)
	}
	if deleting {
		if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.Expires.Before(time.Now()) {
			t.Fatalf("invalid console cookie deletion: %+v", cookie)
		}
		return
	}
	if cookie.Value == "" || cookie.Value == testConsoleToken || cookie.MaxAge <= 0 || cookie.Expires.IsZero() {
		t.Fatalf("invalid live console cookie: %+v", cookie)
	}
}

func TestEveryConsoleSessionIssuanceUsesProductionCookiePolicy(t *testing.T) {
	assertSecureConsoleCookie(t, loginCookie(t, newTestHandler(t)), false)

	handler := newTestHandler(t)
	handler.authorize = func(*http.Request) bool { return true }
	direct := httptest.NewRequest(http.MethodGet, consolePath+"?key=must-not-authorize", nil)
	direct.Header.Set("Authorization", "Bearer recovery-canary")
	directResponse := serveConsole(t, handler, direct)
	if directResponse.Code != http.StatusSeeOther || len(directResponse.Result().Cookies()) != 1 {
		t.Fatalf("direct recovery status=%d cookies=%v", directResponse.Code, directResponse.Result().Cookies())
	}
	assertSecureConsoleCookie(t, directResponse.Result().Cookies()[0], false)
	if strings.Contains(directResponse.Body.String(), "must-not-authorize") || strings.Contains(directResponse.Header().Get("Location"), "key=") {
		t.Fatal("query credential was reflected by bearer recovery")
	}
}

func TestConsoleLogoutRepeatsCookiePolicyForDeletion(t *testing.T) {
	handler := newTestHandler(t)
	live := loginCookie(t, handler)
	request := httptest.NewRequest(http.MethodPost, logoutPath, nil)
	request.AddCookie(live)
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusSeeOther || len(response.Result().Cookies()) != 1 {
		t.Fatalf("logout status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
	assertSecureConsoleCookie(t, response.Result().Cookies()[0], true)
}

func TestConsoleSessionReadsDoNotRefreshOrRenewCookie(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	for _, path := range []string{consolePath, statusPath, cssPath, jsPath} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := serveConsole(t, handler, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, response.Code)
		}
		if len(response.Result().Cookies()) != 0 || response.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s unexpectedly renewed the session cookie", path)
		}
	}
}

func TestExpiredSessionDoesNotIssueReplacementCookie(t *testing.T) {
	now := time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC)
	handler, err := New(Config{
		StaticToken: testConsoleToken,
		Runtime:     Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 78, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Session: SessionConfig{
			TTL:         time.Hour,
			MaxSessions: 4,
			Now:         func() time.Time { return now },
			Rand:        bytes.NewReader(bytes.Repeat([]byte{0x71}, 256)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, handler)
	now = now.Add(time.Hour + time.Second)
	request := httptest.NewRequest(http.MethodGet, statusPath, nil)
	request.AddCookie(cookie)
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
		t.Fatalf("expired session status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}

func TestOAuthStartAndInvalidStateNeverCreateTemporaryCookies(t *testing.T) {
	mux, _ := newOAuthConsoleMux(t)
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest(http.MethodGet, oauthStartPath, nil))
	if start.Code != http.StatusSeeOther || len(start.Result().Cookies()) != 0 {
		t.Fatalf("oauth start status=%d cookies=%v", start.Code, start.Result().Cookies())
	}
	invalid := httptest.NewRecorder()
	mux.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, oauthCallbackPath+"?code=code-value&state=invalid-state", nil))
	if invalid.Code != http.StatusUnauthorized || len(invalid.Result().Cookies()) != 0 {
		t.Fatalf("invalid oauth state status=%d cookies=%v", invalid.Code, invalid.Result().Cookies())
	}
}

func TestOAuthCallbackSessionUsesProductionCookiePolicy(t *testing.T) {
	mux, _ := newOAuthConsoleMux(t)
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest(http.MethodGet, oauthStartPath, nil))
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	form := authorizeURL.Query()
	form.Set("passphrase", "owner-secret")
	authorizeRequest := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorized := httptest.NewRecorder()
	mux.ServeHTTP(authorized, authorizeRequest)
	callback, err := url.Parse(authorized.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	completed := httptest.NewRecorder()
	mux.ServeHTTP(completed, httptest.NewRequest(http.MethodGet, callback.RequestURI(), nil))
	if completed.Code != http.StatusSeeOther || len(completed.Result().Cookies()) != 1 {
		t.Fatalf("oauth callback status=%d cookies=%v", completed.Code, completed.Result().Cookies())
	}
	assertSecureConsoleCookie(t, completed.Result().Cookies()[0], false)
	if strings.Contains(completed.Body.String(), "owner-secret") || strings.Contains(completed.Body.String(), callback.Query().Get("code")) {
		t.Fatal("oauth callback reflected sensitive values")
	}
}
