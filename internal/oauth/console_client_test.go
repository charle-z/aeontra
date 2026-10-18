package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newConsoleProvider(t *testing.T, clientStore string) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		Issuer: "http://localhost:8765", Resource: "http://localhost:8765/mcp",
		Passphrase: "owner-secret", ClientStorePath: clientStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestConsoleClientRegistrationIsDeterministicIdempotentAndPersistent(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "oauth-clients.json")
	provider := newConsoleProvider(t, storePath)
	first, err := provider.NewConsoleClient("/console/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.NewConsoleClient("/console/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	if first.clientID == "" || first.clientID != second.clientID || first.redirectURI != "http://localhost:8765/console/auth/callback" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	info, err := os.Stat(storePath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("client store mode=%v err=%v", info.Mode(), err)
	}
	restarted := newConsoleProvider(t, storePath)
	afterRestart, err := restarted.NewConsoleClient("/console/auth/callback")
	if err != nil || afterRestart.clientID != first.clientID {
		t.Fatalf("restart client=%+v err=%v", afterRestart, err)
	}
}

func TestConsoleClientRejectsInvalidCallbacksAndConflicts(t *testing.T) {
	var nilProvider *Provider
	if _, err := nilProvider.NewConsoleClient("/console/auth/callback"); err == nil {
		t.Fatal("nil provider accepted")
	}
	provider := newConsoleProvider(t, "")
	for _, callback := range []string{"", "relative", "https://example.com/callback", "/callback?code=x", "/callback#fragment", "//other/callback"} {
		if _, err := provider.NewConsoleClient(callback); err == nil {
			t.Fatalf("invalid callback accepted: %q", callback)
		}
	}
	client, err := provider.NewConsoleClient("/console/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	provider.store.clients[client.clientID] = clientReg{redirectURIs: []string{"http://localhost:8765/other"}, createdAt: time.Now()}
	if _, err := provider.NewConsoleClient("/console/auth/callback"); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflict err=%v", err)
	}
	provider.store.clients = make(map[string]clientReg, maxClients)
	for index := 0; index < maxClients; index++ {
		provider.store.clients[strings.Repeat("x", index+1)] = clientReg{redirectURIs: []string{"http://localhost/callback"}, createdAt: time.Now()}
	}
	if _, err := provider.NewConsoleClient("/console/auth/callback"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("limit err=%v", err)
	}
}

func TestConsoleAuthorizationURLContainsExactPKCEStateAndNoPassphrase(t *testing.T) {
	provider := newConsoleProvider(t, "")
	client, err := provider.NewConsoleClient("/console/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ state, challenge string }{{"", "challenge"}, {"state", ""}} {
		if _, err := client.AuthorizationURL(test.state, test.challenge); err == nil {
			t.Fatalf("invalid authorization input accepted: %+v", test)
		}
	}
	var nilClient *ConsoleClient
	if _, err := nilClient.AuthorizationURL("state", "challenge"); err == nil {
		t.Fatal("nil client accepted")
	}
	location, err := client.AuthorizationURL("state-value", "challenge-value")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Path != pathAuthorize || query.Get("client_id") != client.clientID || query.Get("redirect_uri") != client.redirectURI || query.Get("state") != "state-value" || query.Get("code_challenge") != "challenge-value" || query.Get("code_challenge_method") != "S256" || query.Get("scope") != consoleClientScope || query.Get("resource") != provider.resource {
		t.Fatalf("location=%s", location)
	}
	if strings.Contains(strings.ToLower(location), "passphrase") {
		t.Fatalf("passphrase appeared in URL: %s", location)
	}
}

func TestConsoleCompleteConsumesExactCodeAndLeavesNoTokens(t *testing.T) {
	provider := newConsoleProvider(t, "")
	client, err := provider.NewConsoleClient("/console/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	verifier := "console-pkce-verifier-012345678901234567890123456789"
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	put := func(code, clientID, redirectURI, resource, scope, codeChallenge string) {
		provider.store.putCode(code, authCode{clientID: clientID, redirectURI: redirectURI, resource: resource, scope: scope, codeChallenge: codeChallenge, expiresAt: time.Now().Add(time.Minute)})
	}

	var nilClient *ConsoleClient
	if nilClient.Complete("code", verifier) || client.Complete("", verifier) || client.Complete("code", "") || client.Complete("missing", verifier) {
		t.Fatal("invalid completion accepted")
	}
	for _, test := range []struct{ name, clientID, redirectURI, resource, scope, challenge, verifier string }{
		{"client", "other", client.redirectURI, provider.resource, consoleClientScope, challenge, verifier},
		{"redirect", client.clientID, "http://localhost:8765/other", provider.resource, consoleClientScope, challenge, verifier},
		{"resource", client.clientID, client.redirectURI, "http://localhost:8765/other", consoleClientScope, challenge, verifier},
		{"scope", client.clientID, client.redirectURI, provider.resource, "other", challenge, verifier},
		{"pkce", client.clientID, client.redirectURI, provider.resource, consoleClientScope, challenge, "wrong-verifier"},
	} {
		put(test.name, test.clientID, test.redirectURI, test.resource, test.scope, test.challenge)
		if client.Complete(test.name, test.verifier) {
			t.Fatalf("mismatched %s accepted", test.name)
		}
		if _, ok := provider.store.consumeCode(test.name); ok {
			t.Fatalf("mismatched %s code was not consumed", test.name)
		}
	}

	put("valid-code", client.clientID, client.redirectURI, provider.resource, consoleClientScope, challenge)
	if !client.Complete("valid-code", verifier) {
		t.Fatal("valid console code rejected")
	}
	if client.Complete("valid-code", verifier) {
		t.Fatal("authorization code replay accepted")
	}
	if len(provider.store.access) != 0 || len(provider.store.refresh) != 0 {
		t.Fatalf("internal token pair remained stored: access=%d refresh=%d", len(provider.store.access), len(provider.store.refresh))
	}
}

func TestConsoleStoreHelpersFailClosedAndRevoke(t *testing.T) {
	var nilStore *tokenStore
	if err := nilStore.ensureFixedClient("id", "http://localhost/callback"); err == nil {
		t.Fatal("nil store accepted")
	}
	store := newTokenStore()
	for _, test := range []struct{ id, redirect string }{{"", "http://localhost/callback"}, {"id", ""}} {
		if err := store.ensureFixedClient(test.id, test.redirect); err == nil {
			t.Fatalf("invalid fixed client accepted: %+v", test)
		}
	}
	store.putAccess("access", accessGrant{expiresAt: time.Now().Add(time.Minute)})
	store.putRefresh("refresh", refreshGrant{expiresAt: time.Now().Add(time.Minute)})
	store.revokeAccess("")
	store.revokeRefresh("")
	nilStore.revokeAccess("access")
	nilStore.revokeRefresh("refresh")
	store.revokeAccess("access")
	store.revokeRefresh("refresh")
	if len(store.access) != 0 || len(store.refresh) != 0 {
		t.Fatalf("tokens not revoked: access=%d refresh=%d", len(store.access), len(store.refresh))
	}
	if nowUTC().Location() != time.UTC {
		t.Fatal("fixed client timestamps are not UTC")
	}
}
