package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
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

// TestOAuthEndToEnd drives the full OAuth flow over real HTTP against the MCP handler:
// dynamic registration -> owner-passphrase authorize -> PKCE token exchange -> an
// authenticated /mcp call, then confirms a bogus token is rejected with a discovery
// challenge. The logical resource/issuer are fixed (localhost:8765) independent of the
// ephemeral test server address, matching how a deployment is configured.
func TestOAuthEndToEnd(t *testing.T) {
	const (
		passphrase  = "owner-secret-e2e"
		resource    = "http://localhost:8765/mcp"
		redirectURI = "http://127.0.0.1/callback"
	)
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0])
	prov, err := oauth.NewProvider(oauth.Config{
		Issuer:     "http://localhost:8765",
		Resource:   resource,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(svc).HTTPHandler("", prov))
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // don't follow the redirect; we read the code from it
	}}

	// 1) Dynamic client registration.
	regBody, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirectURI}})
	regResp, err := client.Post(ts.URL+"/oauth/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	decodeJSON(t, regResp, &reg)
	if reg.ClientID == "" {
		t.Fatal("registration returned no client_id")
	}

	// 2) PKCE parameters.
	verifier := "e2e-code-verifier-01234567890123456789012345678901"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// 3) Authorize with the owner passphrase -> 302 with an authorization code.
	authForm := url.Values{
		"response_type":         {"code"},
		"client_id":             {reg.ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"e2e-state"},
		"scope":                 {"mcp"},
		"resource":              {resource},
		"passphrase":            {passphrase},
	}
	authResp, err := client.PostForm(ts.URL+"/oauth/authorize", authForm)
	if err != nil {
		t.Fatal(err)
	}
	if authResp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", authResp.StatusCode)
	}
	loc, err := url.Parse(authResp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("authorize returned no code")
	}
	if loc.Query().Get("state") != "e2e-state" {
		t.Errorf("state not preserved: %q", loc.Query().Get("state"))
	}

	// 4) Exchange the code (+PKCE verifier + resource) for an access token.
	tokResp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {reg.ClientID},
		"code_verifier": {verifier},
		"resource":      {resource},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	decodeJSON(t, tokResp, &tok)
	if tok.AccessToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("token exchange failed: %+v", tok)
	}

	// 5) An authenticated MCP call succeeds.
	if code := mcpInitialize(t, client, ts.URL, tok.AccessToken); code == http.StatusUnauthorized {
		t.Fatal("valid OAuth access token was rejected by /mcp")
	}

	// 6) A bogus token is rejected with a discovery challenge.
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	req.Header.Set("Content-Type", "application/json")
	bad, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus token status = %d, want 401", bad.StatusCode)
	}
	if !strings.Contains(bad.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("401 must carry a resource_metadata challenge; got %q", bad.Header.Get("WWW-Authenticate"))
	}
}

func mcpInitialize(t *testing.T, client *http.Client, base, token string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
