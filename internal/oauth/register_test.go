package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func testProviderWithClientStore(t *testing.T, clientStorePath string) *Provider {
	t.Helper()
	p, err := NewProvider(Config{
		Issuer:          "https://mcp.example.com",
		Resource:        "https://mcp.example.com/mcp",
		Passphrase:      "correct horse",
		ClientStorePath: clientStorePath,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
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

func TestRegister_PrunesOnlyExpiredUnactivatedClients(t *testing.T) {
	p := testProvider(t)
	p.store.clients["activated"] = clientReg{
		redirectURIs: []string{"https://active.example.com/cb"},
		createdAt:    time.Now().Add(-2 * unactivatedClientTTL),
		activated:    true,
	}
	for i := 0; i < maxClients-1; i++ {
		p.store.clients[fmt.Sprintf("pending-%03d", i)] = clientReg{
			redirectURIs: []string{"https://pending.example.com/cb"},
			createdAt:    time.Now().Add(-2 * unactivatedClientTTL),
		}
	}

	id, err := p.store.registerClient([]string{"https://new.example.com/cb"})
	if err != nil {
		t.Fatalf("register after pruning: %v", err)
	}
	if id == "" {
		t.Fatal("register returned an empty client id")
	}
	if _, ok := p.store.getClient("activated"); !ok {
		t.Fatal("expired activated client was pruned")
	}
	p.store.mu.Lock()
	clientCount := len(p.store.clients)
	p.store.mu.Unlock()
	if clientCount != 2 {
		t.Fatalf("client count after pruning = %d, want 2", clientCount)
	}
}

func TestExpiredUnactivatedClientCannotAuthorize(t *testing.T) {
	p := testProvider(t)
	p.store.clients["expired-pending"] = clientReg{
		redirectURIs: []string{"https://pending.example.com/cb"},
		createdAt:    time.Now().Add(-2 * unactivatedClientTTL),
	}
	p.store.failTimes["expired-pending"] = []time.Time{time.Now()}

	if _, ok := p.store.getClient("expired-pending"); ok {
		t.Fatal("expired unactivated client remained valid")
	}
	if _, ok := p.store.failTimes["expired-pending"]; ok {
		t.Fatal("expired client throttle state was retained")
	}
}

func TestClientStoreVersionOneMigratesClientsAsActivated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-clients.json")
	createdAt := time.Now().Add(-2 * unactivatedClientTTL).UTC()
	body := fmt.Sprintf(`{"version":1,"clients":[{"id":"legacy","redirect_uris":["https://legacy.example.com/cb"],"created_at":%q}]}`, createdAt.Format(time.RFC3339Nano))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	p := testProviderWithClientStore(t, path)
	client, ok := p.store.getClient("legacy")
	if !ok || !client.activated {
		t.Fatalf("version 1 client not migrated as activated: ok=%t client=%+v", ok, client)
	}
}

func TestAuthorizationActivationFailsClosedWhenClientStoreCannotPersist(t *testing.T) {
	p := testProvider(t)
	clientID, err := p.store.registerClient([]string{"https://client.example.com/cb"})
	if err != nil {
		t.Fatal(err)
	}
	p.store.mu.Lock()
	p.store.clientStorePath = t.TempDir()
	p.store.mu.Unlock()

	if err := p.store.putCode("code", authCode{clientID: clientID, expiresAt: time.Now().Add(time.Minute)}); err == nil {
		t.Fatal("authorization succeeded when client activation could not persist")
	}
	client, ok := p.store.getClient(clientID)
	if !ok || client.activated {
		t.Fatalf("failed activation changed client state: ok=%t client=%+v", ok, client)
	}
	if _, ok := p.store.consumeCode("code"); ok {
		t.Fatal("failed activation exposed an authorization code")
	}
}

func TestAuthorizationMarksDynamicClientActivated(t *testing.T) {
	p := testProvider(t)
	clientID, err := p.store.registerClient([]string{"https://client.example.com/cb"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.putCode("code", authCode{clientID: clientID, expiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	client, ok := p.store.getClient(clientID)
	if !ok || !client.activated {
		t.Fatalf("authorized client was not activated: ok=%t client=%+v", ok, client)
	}
}

func TestRegister_PersistentClientsSurviveProviderRestart(t *testing.T) {
	clientStorePath := filepath.Join(t.TempDir(), "oauth-clients.json")
	p1 := testProviderWithClientStore(t, clientStorePath)

	rec := postJSON(t, p1, pathRegister, `{
		"client_name": "ChatGPT",
		"redirect_uris": ["https://chatgpt.com/connector_platform_oauth_redirect"]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.ClientID == "" || len(doc.RedirectURIs) != 1 {
		t.Fatalf("bad registration response: %+v", doc)
	}

	p2 := testProviderWithClientStore(t, clientStorePath)
	auth := getAuthorize(t, p2, validAuthorizeParams(p2, doc.ClientID, doc.RedirectURIs[0]))
	if auth.Code != http.StatusOK {
		t.Fatalf("authorize after restart status = %d, want 200; body=%s", auth.Code, auth.Body.String())
	}
}

func TestRegister_PersistenceDoesNotPersistTokens(t *testing.T) {
	clientStorePath := filepath.Join(t.TempDir(), "oauth-clients.json")
	p1 := testProviderWithClientStore(t, clientStorePath)
	clientID, _ := regTestClient(t, p1)

	access := p1.issueAccessToken(clientID, p1.resource, "mcp", time.Hour)
	refresh := randToken()
	p1.store.putRefresh(refresh, refreshGrant{
		clientID:  clientID,
		scope:     "mcp",
		resource:  p1.resource,
		expiresAt: time.Now().Add(refreshTokenTTL),
	})

	p2 := testProviderWithClientStore(t, clientStorePath)
	if p2.Authorize(bearerReq(access)) {
		t.Fatal("access tokens must remain in-memory only across provider restart")
	}
	if _, ok, _ := p2.store.consumeRefresh(refresh); ok {
		t.Fatal("refresh tokens must remain in-memory only across provider restart")
	}
	body, err := os.ReadFile(clientStorePath)
	if err != nil {
		t.Fatalf("read client store: %v", err)
	}
	if strings.Contains(string(body), access) || strings.Contains(string(body), refresh) {
		t.Fatalf("client store must not contain bearer tokens: %s", body)
	}
}
