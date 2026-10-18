package console

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/oauth"
)

func TestBearerRecoverySessionSurvivesHandlerRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	cfg := Config{
		StaticToken: testConsoleToken,
		Runtime:     Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 78, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Session: SessionConfig{
			Path: path, TTL: time.Hour, MaxSessions: 8,
			Rand: bytes.NewReader(bytes.Repeat([]byte{0x61}, sessionBytes)),
		},
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, first)
	if err := first.sessions.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Session.Rand = nil
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.sessions.Close()
	request := httptest.NewRequest(http.MethodGet, statusPath, nil)
	request.AddCookie(cookie)
	response := serveConsole(t, second, request)
	if response.Code != http.StatusOK {
		t.Fatalf("restarted bearer session status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOAuthSessionSurvivesHandlerRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "console", "sessions.db")
	provider := newRestartOAuthProvider(t)
	cfg := Config{
		OAuthProvider: provider,
		Runtime:       Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 78, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Session: SessionConfig{
			Path: path, TTL: time.Hour, MaxSessions: 8,
			Rand: bytes.NewReader(bytes.Repeat([]byte{0x62}, 256)),
		},
	}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	provider.RegisterRoutes(mux)
	first.Register(mux)
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
	cookies := completed.Result().Cookies()
	if completed.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("oauth completion status=%d cookies=%v", completed.Code, cookies)
	}
	if err := first.sessions.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.OAuthProvider = newRestartOAuthProvider(t)
	cfg.Session.Rand = nil
	second, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.sessions.Close()
	request := httptest.NewRequest(http.MethodGet, statusPath, nil)
	request.AddCookie(cookies[0])
	response := serveConsole(t, second, request)
	if response.Code != http.StatusOK {
		t.Fatalf("restarted oauth session status=%d body=%s", response.Code, response.Body.String())
	}
}

func newRestartOAuthProvider(t *testing.T) *oauth.Provider {
	t.Helper()
	provider, err := oauth.NewProvider(oauth.Config{
		Issuer: "http://localhost:8765", Resource: "http://localhost:8765/mcp", Passphrase: "owner-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}
