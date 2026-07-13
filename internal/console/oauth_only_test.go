package console

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthOnlyConsoleDoesNotRenderStaticTokenForm(t *testing.T) {
	handler, err := New(Config{
		Runtime: Status{Status: "ok", Version: "0.2.0", ProtocolVersion: "2024-11-05", Commit: "abcdef0", ToolCount: 62, CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Authorize: func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer oauth-access"
		},
		Session: SessionConfig{TTL: time.Hour, MaxSessions: 4, Rand: bytes.NewReader(bytes.Repeat([]byte{0x61}, 256))},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := serveConsole(t, handler, httptest.NewRequest(http.MethodGet, consolePath, nil))
	if page.Code != http.StatusOK {
		t.Fatalf("status=%d", page.Code)
	}
	body := page.Body.String()
	if strings.Contains(body, `name="token"`) || strings.Contains(body, `type="password"`) {
		t.Fatalf("OAuth-only page rendered token form: %s", body)
	}
	if !strings.Contains(body, "OAuth or bearer authentication is required") {
		t.Fatalf("OAuth-only guidance missing: %s", body)
	}

	request := httptest.NewRequest(http.MethodGet, consolePath, nil)
	request.Header.Set("Authorization", "Bearer oauth-access")
	response := serveConsole(t, handler, request)
	if response.Code != http.StatusSeeOther || len(response.Result().Cookies()) != 1 {
		t.Fatalf("direct OAuth bootstrap status=%d cookies=%v", response.Code, response.Result().Cookies())
	}
}
