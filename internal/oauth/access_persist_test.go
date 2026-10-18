package oauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func providerWithAccessStore(t *testing.T, accessPath string) *Provider {
	t.Helper()
	p, err := NewProvider(Config{
		Issuer:          "https://mcp.example.com",
		Resource:        "https://mcp.example.com/mcp",
		Passphrase:      "correct horse",
		AccessStorePath: accessPath,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

func TestAccessGrantSurvivesProviderRestartWithoutPersistingBearer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	p1 := providerWithAccessStore(t, path)
	access := p1.issueAccessToken("client-1", p1.resource, "mcp", time.Hour)
	if access == "" {
		t.Fatal("access token was not issued")
	}

	p2 := providerWithAccessStore(t, path)
	principal, ok := p2.Principal(bearerReq(access))
	if !ok || principal != "oauth-client:client-1" {
		t.Fatalf("persisted access grant principal=%q ok=%v", principal, ok)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read access store: %v", err)
	}
	if strings.Contains(string(body), access) {
		t.Fatal("access store contains the raw bearer")
	}
	var doc struct {
		Version int `json:"version"`
		Access  []struct {
			Digest string `json:"digest"`
		} `json:"access"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode access store: %v", err)
	}
	if doc.Version != 1 || len(doc.Access) != 1 || len(doc.Access[0].Digest) != 64 {
		t.Fatalf("unexpected access store document: version=%d records=%d digest_len=%d", doc.Version, len(doc.Access), len(doc.Access[0].Digest))
	}
}

func TestAccessGrantExpiredRecordIsNotRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	p1 := providerWithAccessStore(t, path)
	access := p1.issueAccessToken("client-1", p1.resource, "mcp", -time.Second)
	if access == "" {
		t.Fatal("expired fixture token was not issued")
	}
	p2 := providerWithAccessStore(t, path)
	if p2.Authorize(bearerReq(access)) {
		t.Fatal("expired access grant survived provider restart")
	}
}

func TestAccessStoreFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not enforced on Windows; the deploy target is Linux")
	}
	path := filepath.Join(t.TempDir(), "oauth-access.json")
	p := providerWithAccessStore(t, path)
	if token := p.issueAccessToken("client-1", p.resource, "mcp", time.Hour); token == "" {
		t.Fatal("access token was not issued")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("access store perm=%o want=600", perm)
	}
	lockInfo, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if perm := lockInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("access lock perm=%o want=600", perm)
	}
}

func TestNewProviderRejectsRelativeAccessStorePath(t *testing.T) {
	_, err := NewProvider(Config{
		Issuer:          "https://mcp.example.com",
		Resource:        "https://mcp.example.com/mcp",
		Passphrase:      "correct horse",
		AccessStorePath: "relative.json",
	})
	if err == nil {
		t.Fatal("relative AccessStorePath was accepted")
	}
}
