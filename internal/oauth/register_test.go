package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postJSON(t *testing.T, p *Provider, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRegister_Success(t *testing.T) {
	p := testProvider(t)
	rec := postJSON(t, p, pathRegister, `{
		"client_name": "ChatGPT",
		"redirect_uris": ["https://chatgpt.com/connector_platform_oauth_redirect"],
		"grant_types": ["authorization_code","refresh_token"],
		"response_types": ["code"],
		"token_endpoint_auth_method": "none"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var doc struct {
		ClientID                string   `json:"client_id"`
		ClientSecret            string   `json:"client_secret"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.ClientID == "" {
		t.Error("client_id must be non-empty")
	}
	if doc.ClientSecret != "" {
		t.Error("public client must not receive a client_secret")
	}
	if doc.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want none", doc.TokenEndpointAuthMethod)
	}
	// The client must be retrievable for the authorize step.
	if _, ok := p.store.getClient(doc.ClientID); !ok {
		t.Error("registered client should be stored")
	}
}

func TestRegister_RejectsBadRedirectURIs(t *testing.T) {
	p := testProvider(t)
	bad := []string{
		`{"redirect_uris": ["http://evil.example.com/cb"]}`,     // http non-localhost
		`{"redirect_uris": ["https://ok.example.com/cb#frag"]}`, // fragment
		`{"redirect_uris": ["https://ok.example.com/*"]}`,       // wildcard
		`{"redirect_uris": ["ftp://ok.example.com/cb"]}`,        // wrong scheme
		`{"redirect_uris": []}`,                                 // none
		`{"client_name":"x"}`,                                   // missing redirect_uris
	}
	for _, body := range bad {
		rec := postJSON(t, p, pathRegister, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestRegister_AllowsLocalhostHTTPRedirect(t *testing.T) {
	p := testProvider(t)
	rec := postJSON(t, p, pathRegister, `{"redirect_uris": ["http://localhost:8080/callback"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("localhost http redirect should be allowed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegister_MalformedJSON(t *testing.T) {
	p := testProvider(t)
	rec := postJSON(t, p, pathRegister, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRegister_MethodNotAllowed(t *testing.T) {
	p := testProvider(t)
	rec := serve(t, p, http.MethodGet, pathRegister)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestRegister_CapEnforced(t *testing.T) {
	p := testProvider(t)
	// Exceeding the registration cap must eventually be rejected (429), never unbounded.
	got429 := false
	for i := 0; i < maxClients+maxRegistrationsPerWindow+5; i++ {
		rec := postJSON(t, p, pathRegister, `{"redirect_uris": ["https://ok.example.com/cb"]}`)
		if rec.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("registration must be rate/cap limited (expected a 429)")
	}
}
