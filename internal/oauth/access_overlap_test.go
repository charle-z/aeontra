package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAccessStoreReloadsGrantIssuedAfterPeerStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	issuer := providerWithAccessStore(t, path)
	peer := providerWithAccessStore(t, path)

	token := issuer.issueAccessToken("client-late", issuer.resource, "mcp", time.Hour)
	if token == "" {
		t.Fatal("issuer did not create a token")
	}
	principal, ok := peer.Principal(bearerReq(token))
	if !ok || principal != "oauth-client:client-late" {
		t.Fatalf("peer did not reload late grant: principal=%q ok=%v", principal, ok)
	}
}

func TestAccessStoreConcurrentProvidersDoNotLoseGrants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	first := providerWithAccessStore(t, path)
	second := providerWithAccessStore(t, path)

	const perProvider = 32
	tokens := make(chan string, perProvider*2)
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	issue := func(provider *Provider, prefix string) {
		defer wg.Done()
		for i := 0; i < perProvider; i++ {
			token := provider.issueAccessToken(fmt.Sprintf("%s-%d", prefix, i), provider.resource, "mcp", time.Hour)
			if token == "" {
				errors <- fmt.Errorf("%s token %d was not issued", prefix, i)
				return
			}
			tokens <- token
		}
	}
	wg.Add(2)
	go issue(first, "first")
	go issue(second, "second")
	wg.Wait()
	close(tokens)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}

	restarted := providerWithAccessStore(t, path)
	count := 0
	for token := range tokens {
		if !restarted.Authorize(bearerReq(token)) {
			t.Fatalf("concurrent grant %d was lost", count)
		}
		count++
	}
	if count != perProvider*2 {
		t.Fatalf("validated %d grants, want %d", count, perProvider*2)
	}
}

func TestAccessStoreRejectsMalformedDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	body, err := json.Marshal(accessStoreFile{Version: 1, Access: []accessStoreRecord{{
		Digest: "not-a-digest", ClientID: "client", Scope: "mcp", Resource: "https://mcp.example.com/mcp", ExpiresAt: time.Now().Add(time.Hour),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{
		Issuer: "https://mcp.example.com", Resource: "https://mcp.example.com/mcp", Passphrase: "correct horse", AccessStorePath: path,
	}); err == nil {
		t.Fatal("provider accepted a malformed access digest")
	}
}

func TestAccessStoreRejectsOversizedDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	records := make([]accessStoreRecord, maxAccessGrants+1)
	for i := range records {
		records[i] = accessStoreRecord{
			Digest: accessTokenDigest(fmt.Sprintf("fixture-%d", i)), ClientID: "client", Scope: "mcp",
			Resource: "https://mcp.example.com/mcp", ExpiresAt: time.Now().Add(time.Hour),
		}
	}
	body, err := json.Marshal(accessStoreFile{Version: 1, Access: records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{
		Issuer: "https://mcp.example.com", Resource: "https://mcp.example.com/mcp", Passphrase: "correct horse", AccessStorePath: path,
	}); err == nil {
		t.Fatal("provider accepted an oversized access store")
	}
}

func TestAccessStoreWriteFailurePreventsTokenIssuance(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "oauth-access.json")
	provider := providerWithAccessStore(t, path)

	if err := os.Rename(state, state+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token := provider.issueAccessToken("client", provider.resource, "mcp", time.Hour); token != "" {
		t.Fatal("token was issued even though the access store could not be locked")
	}
}
