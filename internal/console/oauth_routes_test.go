package console

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/oauth"
)

func newOAuthConsoleMux(t *testing.T) (*http.ServeMux, *Handler) {
	t.Helper()
	provider, err := oauth.NewProvider(oauth.Config{
		Issuer: "http://localhost:8765", Resource: "http://localhost:8765/mcp", Passphrase: "owner-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{
		OAuthProvider: provider,
		SecureCookies: true,
		Runtime:       Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 67, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Session:       SessionConfig{TTL: time.Hour, MaxSessions: 4, Rand: bytes.NewReader(bytes.Repeat([]byte{0x51}, 256))},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	provider.RegisterRoutes(mux)
	handler.Register(mux)
	return mux, handler
}

func TestConsoleOAuthRoutesCompleteAndRejectReplay(t *testing.T) {
	mux, _ := newOAuthConsoleMux(t)
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest(http.MethodGet, oauthStartPath, nil))
	if start.Code != http.StatusSeeOther {
		t.Fatalf("start=%d body=%s", start.Code, start.Body.String())
	}
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil || authorizeURL.Path != "/oauth/authorize" || authorizeURL.Query().Get("state") == "" || authorizeURL.Query().Get("code_challenge") == "" {
		t.Fatalf("authorize URL=%v err=%v", authorizeURL, err)
	}
	if strings.Contains(strings.ToLower(authorizeURL.String()), "passphrase") {
		t.Fatal("passphrase appeared in URL")
	}
	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest(http.MethodGet, authorizeURL.RequestURI(), nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="passphrase"`) {
		t.Fatalf("authorize page=%d body=%s", page.Code, page.Body.String())
	}

	form := authorizeURL.Query()
	form.Set("passphrase", "owner-secret")
	request := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authorized := httptest.NewRecorder()
	mux.ServeHTTP(authorized, request)
	callback, err := url.Parse(authorized.Header().Get("Location"))
	if err != nil || callback.Path != oauthCallbackPath {
		t.Fatalf("callback=%v err=%v status=%d", callback, err, authorized.Code)
	}

	completed := httptest.NewRecorder()
	mux.ServeHTTP(completed, httptest.NewRequest(http.MethodGet, callback.RequestURI(), nil))
	if completed.Code != http.StatusSeeOther || completed.Header().Get("Location") != consolePath {
		t.Fatalf("callback status=%d location=%q", completed.Code, completed.Header().Get("Location"))
	}
	cookies := completed.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies=%+v", cookies)
	}

	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, callback.RequestURI(), nil))
	if replay.Code != http.StatusUnauthorized || len(replay.Result().Cookies()) != 0 {
		t.Fatalf("replay status=%d cookies=%v", replay.Code, replay.Result().Cookies())
	}
}

func TestConsoleOAuthRoutesFailClosedOnInvalidRequests(t *testing.T) {
	mux, _ := newOAuthConsoleMux(t)
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, oauthStartPath, http.StatusMethodNotAllowed},
		{http.MethodPost, oauthCallbackPath, http.StatusMethodNotAllowed},
		{http.MethodGet, oauthCallbackPath, http.StatusUnauthorized},
		{http.MethodGet, oauthCallbackPath + "?code=x&state=y&extra=z", http.StatusUnauthorized},
		{http.MethodGet, consolePath + "/unknown", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.want {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.path, response.Code, test.want)
		}
	}
	redirect := httptest.NewRecorder()
	mux.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, consolePath+"/", nil))
	if redirect.Code != http.StatusPermanentRedirect {
		t.Fatalf("console slash redirect=%d", redirect.Code)
	}
}
