package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testProvider builds a Provider with a fixed public URL for deterministic metadata.
func testProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := NewProvider(Config{
		Issuer:     "https://mcp.example.com",
		Resource:   "https://mcp.example.com/mcp",
		Passphrase: "correct horse",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

// serve routes a request through a mux with the provider's routes registered.
func serve(t *testing.T, p *Provider, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestNewProvider_RejectsBadConfig(t *testing.T) {
	cases := []Config{
		{Issuer: "", Resource: "https://h/mcp", Passphrase: "x"},                // no issuer
		{Issuer: "https://h", Resource: "", Passphrase: "x"},                    // no resource
		{Issuer: "https://h", Resource: "https://h/mcp", Passphrase: ""},        // no passphrase
		{Issuer: "http://h", Resource: "http://h/mcp", Passphrase: "x"},         // not https
		{Issuer: "https://h/", Resource: "https://h/mcp#frag", Passphrase: "x"}, // fragment in resource
	}
	for i, c := range cases {
		if _, err := NewProvider(c); err == nil {
			t.Errorf("case %d: expected error for %+v, got nil", i, c)
		}
	}
}

func TestNewProvider_AllowsLocalhostHTTP(t *testing.T) {
	// localhost dev is exempt from the https requirement.
	if _, err := NewProvider(Config{
		Issuer:     "http://localhost:8765",
		Resource:   "http://localhost:8765/mcp",
		Passphrase: "x",
	}); err != nil {
		t.Fatalf("localhost http should be allowed: %v", err)
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	p := testProvider(t)
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		rec := serve(t, p, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content-type = %q, want application/json", path, ct)
		}
		var doc struct {
			Resource               string   `json:"resource"`
			AuthorizationServers   []string `json:"authorization_servers"`
			ScopesSupported        []string `json:"scopes_supported"`
			BearerMethodsSupported []string `json:"bearer_methods_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: unmarshal: %v", path, err)
		}
		if doc.Resource != "https://mcp.example.com/mcp" {
			t.Errorf("%s: resource = %q", path, doc.Resource)
		}
		if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://mcp.example.com" {
			t.Errorf("%s: authorization_servers = %v", path, doc.AuthorizationServers)
		}
		if len(doc.BearerMethodsSupported) != 1 || doc.BearerMethodsSupported[0] != "header" {
			t.Errorf("%s: bearer_methods_supported = %v (must be header-only)", path, doc.BearerMethodsSupported)
		}
	}
}

func TestAuthorizationServerMetadata(t *testing.T) {
	p := testProvider(t)
	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	} {
		rec := serve(t, p, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
		var doc struct {
			Issuer                        string   `json:"issuer"`
			AuthorizationEndpoint         string   `json:"authorization_endpoint"`
			TokenEndpoint                 string   `json:"token_endpoint"`
			RegistrationEndpoint          string   `json:"registration_endpoint"`
			ResponseTypesSupported        []string `json:"response_types_supported"`
			GrantTypesSupported           []string `json:"grant_types_supported"`
			CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
			TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: unmarshal: %v", path, err)
		}
		if doc.Issuer != "https://mcp.example.com" {
			t.Errorf("%s: issuer = %q", path, doc.Issuer)
		}
		if doc.AuthorizationEndpoint != "https://mcp.example.com/oauth/authorize" {
			t.Errorf("%s: authorization_endpoint = %q", path, doc.AuthorizationEndpoint)
		}
		if doc.TokenEndpoint != "https://mcp.example.com/oauth/token" {
			t.Errorf("%s: token_endpoint = %q", path, doc.TokenEndpoint)
		}
		if doc.RegistrationEndpoint != "https://mcp.example.com/oauth/register" {
			t.Errorf("%s: registration_endpoint = %q", path, doc.RegistrationEndpoint)
		}
		// PKCE S256 only — plain must never be advertised.
		if len(doc.CodeChallengeMethodsSupported) != 1 || doc.CodeChallengeMethodsSupported[0] != "S256" {
			t.Errorf("%s: code_challenge_methods_supported = %v, want [S256] only", path, doc.CodeChallengeMethodsSupported)
		}
		if len(doc.TokenEndpointAuthMethods) != 1 || doc.TokenEndpointAuthMethods[0] != "none" {
			t.Errorf("%s: token_endpoint_auth_methods_supported = %v, want [none]", path, doc.TokenEndpointAuthMethods)
		}
	}
}

func TestMetadata_MethodNotAllowed(t *testing.T) {
	p := testProvider(t)
	rec := serve(t, p, http.MethodPost, "/.well-known/oauth-authorization-server")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST metadata: status = %d, want 405", rec.Code)
	}
}
