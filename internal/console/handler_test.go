package console

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const testConsoleToken = "console-test-token-with-sufficient-entropy"

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	now := time.Date(2026, 7, 13, 21, 0, 0, 0, time.UTC)
	random := bytes.Repeat([]byte{0x33}, 4096)
	handler, err := New(Config{
		StaticToken: testConsoleToken,
		Runtime: Status{
			Status:          "ok",
			Version:         "0.2.0",
			ProtocolVersion: "2024-11-05",
			Commit:          "0123456789abcdef",
			ToolCount:       62,
			CatalogHash:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Authorize: func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer direct-authorized"
		},
		Session: SessionConfig{
			TTL:         8 * time.Hour,
			MaxSessions: 8,
			Now:         func() time.Time { return now },
			Rand:        bytes.NewReader(random),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func serveConsole(t *testing.T, handler *Handler, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func loginCookie(t *testing.T, handler *Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"token": {testConsoleToken}}
	request := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	return cookies[0]
}

func TestUnauthenticatedConsoleShowsMinimalLoginOnly(t *testing.T) {
	handler := newTestHandler(t)
	response := serveConsole(t, handler, httptest.NewRequest(http.MethodGet, consolePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	body := response.Body.String()
	for _, required := range []string{"Sign in", "MCP Devbox Console", "type=\"password\""} {
		if !strings.Contains(body, required) {
			t.Fatalf("login missing %q: %s", required, body)
		}
	}
	for _, forbidden := range []string{"0123456789abcdef", "tool_count", "catalog_hash", testConsoleToken} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("login leaked %q: %s", forbidden, body)
		}
	}
	assertConsoleHeaders(t, response.Header(), true)
}

func TestBadLoginIsGenericAndDoesNotLeakSecrets(t *testing.T) {
	handler := newTestHandler(t)
	secret := "wrong-token-customer-secret"
	form := url.Values{"token": {secret}}
	request := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, secret) || strings.Contains(body, testConsoleToken) {
		t.Fatalf("login response leaked secret: %s", body)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("failed login created a cookie")
	}
}

func TestSuccessfulLoginCreatesOpaqueScopedCookie(t *testing.T) {
	cookie := loginCookie(t, newTestHandler(t))
	if cookie.Name != cookieName || cookie.Value == "" || cookie.Value == testConsoleToken {
		t.Fatalf("cookie=%+v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security=%+v", cookie)
	}
	if cookie.Path != "/" {
		t.Fatalf("cookie path=%q", cookie.Path)
	}
	if cookie.MaxAge <= 0 || cookie.Expires.IsZero() {
		t.Fatalf("cookie expiry=%+v", cookie)
	}
}

func TestAuthenticatedConsoleAndAssetsRequireSession(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	request := httptest.NewRequest(http.MethodGet, consolePath, nil)
	request.AddCookie(cookie)
	page := serveConsole(t, handler, request)
	if page.Code != http.StatusOK {
		t.Fatalf("page status=%d", page.Code)
	}
	for _, required := range []string{"app.css", "app.js", `id="root"`, "Operations Firmware"} {
		if !strings.Contains(page.Body.String(), required) {
			t.Fatalf("page missing %q", required)
		}
	}
	assertConsoleHeaders(t, page.Header(), false)

	for _, path := range []string{cssPath, jsPath, statusPath} {
		unauthorized := serveConsole(t, handler, httptest.NewRequest(http.MethodGet, path, nil))
		if unauthorized.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized %s status=%d", path, unauthorized.Code)
		}
		authorizedRequest := httptest.NewRequest(http.MethodGet, path, nil)
		authorizedRequest.AddCookie(cookie)
		authorized := serveConsole(t, handler, authorizedRequest)
		if authorized.Code != http.StatusOK {
			t.Fatalf("authorized %s status=%d", path, authorized.Code)
		}
	}
}

func TestStatusUsesExactSafeSchema(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	request := httptest.NewRequest(http.MethodGet, statusPath, nil)
	request.AddCookie(cookie)
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"authenticated", "catalog_hash", "commit", "protocol_version", "status", "surface", "tool_count", "version"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("keys=%v want=%v", keys, want)
	}
	encoded := response.Body.String()
	for _, forbidden := range []string{"path", "repo", "prompt", "target", "token", "identity", "error", testConsoleToken} {
		if strings.Contains(strings.ToLower(encoded), forbidden) {
			t.Fatalf("status leaked forbidden marker %q: %s", forbidden, encoded)
		}
	}
}

func TestDirectAuthorizationBootstrapsSessionAndRemovesQuery(t *testing.T) {
	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, consolePath+"?key=should-not-survive", nil)
	request.Header.Set("Authorization", "Bearer direct-authorized")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != consolePath {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	if len(response.Result().Cookies()) != 1 {
		t.Fatal("direct auth did not create console session")
	}
	if strings.Contains(response.Body.String(), "should-not-survive") {
		t.Fatal("query secret reflected")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	request := httptest.NewRequest(http.MethodPost, logoutPath, nil)
	request.AddCookie(cookie)
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("logout status=%d", response.Code)
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("clear cookie=%+v", cleared)
	}
	statusRequest := httptest.NewRequest(http.MethodGet, statusPath, nil)
	statusRequest.AddCookie(cookie)
	if got := serveConsole(t, handler, statusRequest).Code; got != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d", got)
	}
}

func TestMethodsBodiesAndContentTypesFailClosed(t *testing.T) {
	handler := newTestHandler(t)
	cases := []struct {
		method string
		path   string
		body   io.Reader
		want   int
	}{
		{http.MethodPut, consolePath, nil, http.StatusMethodNotAllowed},
		{http.MethodGet, loginPath, nil, http.StatusMethodNotAllowed},
		{http.MethodPost, statusPath, nil, http.StatusMethodNotAllowed},
		{http.MethodPost, cssPath, nil, http.StatusMethodNotAllowed},
		{http.MethodPost, loginPath, strings.NewReader(strings.Repeat("x", maxLoginBody+1)), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(tc.method, tc.path, tc.body)
		if tc.path == loginPath {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response := serveConsole(t, handler, request)
		if response.Code != tc.want {
			t.Errorf("%s %s status=%d want=%d", tc.method, tc.path, response.Code, tc.want)
		}
	}

	request := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader("token=x"))
	request.Header.Set("Content-Type", "application/json")
	if got := serveConsole(t, handler, request).Code; got != http.StatusUnsupportedMediaType {
		t.Fatalf("content type status=%d", got)
	}
}

func TestResponsesNeverContainTokenOrCookieCanaries(t *testing.T) {
	handler := newTestHandler(t)
	cookie := loginCookie(t, handler)
	for _, path := range []string{consolePath, statusPath, cssPath, jsPath} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(cookie)
		response := serveConsole(t, handler, request)
		body := response.Body.String()
		for _, secret := range []string{testConsoleToken, cookie.Value} {
			if strings.Contains(body, secret) {
				t.Fatalf("%s leaked secret: %s", path, body)
			}
		}
	}
}

func assertConsoleHeaders(t *testing.T, headers http.Header, login bool) {
	t.Helper()
	for _, name := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Cache-Control",
	} {
		if strings.TrimSpace(headers.Get(name)) == "" {
			t.Errorf("missing %s", name)
		}
	}
	csp := headers.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("weak CSP: %s", csp)
	}
	if strings.Contains(csp, "http:") || strings.Contains(csp, "https:") || strings.Contains(csp, "*") {
		t.Fatalf("external CSP source: %s", csp)
	}
	if !login && (!strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'")) {
		t.Fatalf("console CSP missing same-origin directives: %s", csp)
	}
}
