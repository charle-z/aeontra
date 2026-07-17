package mcpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func newConsoleOAuthHandler(t *testing.T) http.Handler {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := oauth.NewProvider(oauth.Config{
		Issuer:     "http://localhost:8765",
		Resource:   "http://localhost:8765/mcp",
		Passphrase: "console-owner-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0])
	return New(service).HTTPHandlerWithOptions("", provider, HTTPOptions{})
}

func performConsoleOAuth(t *testing.T, handler http.Handler) (*http.Cookie, string) {
	t.Helper()
	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/console/auth/start", nil))
	if start.Code != http.StatusSeeOther {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Path != "/oauth/authorize" || authorizeURL.Query().Get("state") == "" || authorizeURL.Query().Get("code_challenge") == "" {
		t.Fatalf("invalid authorize redirect: %s", authorizeURL.String())
	}
	if strings.Contains(strings.ToLower(authorizeURL.String()), "passphrase") {
		t.Fatalf("passphrase leaked into authorize URL: %s", authorizeURL.String())
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodGet, authorizeURL.RequestURI(), nil))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), `name="passphrase"`) {
		t.Fatalf("authorize page status=%d body=%s", login.Code, login.Body.String())
	}

	form := authorizeURL.Query()
	form.Set("passphrase", "console-owner-secret")
	authorizeRequest := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	authorizeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizeRequest)
	if authorized.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	callbackURL, err := url.Parse(authorized.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if callbackURL.Path != "/console/auth/callback" || callbackURL.Query().Get("code") == "" || callbackURL.Query().Get("state") == "" {
		t.Fatalf("invalid callback redirect: %s", callbackURL.String())
	}

	callback := httptest.NewRecorder()
	handler.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, callbackURL.RequestURI(), nil))
	if callback.Code != http.StatusSeeOther || callback.Header().Get("Location") != "/console" {
		t.Fatalf("callback status=%d location=%q body=%s", callback.Code, callback.Header().Get("Location"), callback.Body.String())
	}
	cookies := callback.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("callback cookies=%v", cookies)
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == "" {
		t.Fatalf("insecure console cookie: %+v", cookie)
	}
	for _, secret := range []string{"console-owner-secret", callbackURL.Query().Get("code"), callbackURL.Query().Get("state")} {
		if secret != "" && strings.Contains(callback.Body.String(), secret) {
			t.Fatalf("callback leaked secret material: %s", callback.Body.String())
		}
	}
	return cookie, callbackURL.RequestURI()
}

func TestConsoleOAuthFlowCreatesOpaqueSessionAndRejectsReplay(t *testing.T) {
	handler := newConsoleOAuthHandler(t)
	cookie, callbackURI := performConsoleOAuth(t, handler)

	statusRequest := httptest.NewRequest(http.MethodGet, "/console/status", nil)
	statusRequest.AddCookie(cookie)
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, statusRequest)
	if status.Code != http.StatusOK {
		t.Fatalf("console status=%d body=%s", status.Code, status.Body.String())
	}

	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, callbackURI, nil))
	if replay.Code != http.StatusUnauthorized || len(replay.Result().Cookies()) != 0 {
		t.Fatalf("callback replay status=%d cookies=%v", replay.Code, replay.Result().Cookies())
	}
}

func TestConsoleOAuthCallbackBindsStateToPKCEVerifier(t *testing.T) {
	handler := newConsoleOAuthHandler(t)

	startFlow := func() *url.URL {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/console/auth/start", nil))
		parsed, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	first := startFlow()
	second := startFlow()

	form := first.Query()
	form.Set("passphrase", "console-owner-secret")
	request := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, request)
	callback, err := url.Parse(authorized.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callbackQuery := callback.Query()
	callbackQuery.Set("state", second.Query().Get("state"))
	callback.RawQuery = callbackQuery.Encode()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, callback.RequestURI(), nil))
	if response.Code != http.StatusUnauthorized || len(response.Result().Cookies()) != 0 {
		t.Fatalf("mismatched PKCE/state status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}
