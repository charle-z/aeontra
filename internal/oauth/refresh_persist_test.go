package oauth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func providerWithRefreshStore(t *testing.T, refreshPath string) *Provider {
	t.Helper()
	p, err := NewProvider(Config{
		Issuer:           "https://mcp.example.com",
		Resource:         "https://mcp.example.com/mcp",
		Passphrase:       "correct horse",
		RefreshStorePath: refreshPath,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

func TestRefresh_SurvivesProviderRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p1 := providerWithRefreshStore(t, path)

	refresh := randToken()
	p1.store.putRefresh(refresh, refreshGrant{
		clientID:  "client-1",
		scope:     "mcp",
		resource:  p1.resource,
		expiresAt: time.Now().Add(refreshTokenTTL),
	})

	// A fresh Provider pointed at the same file must still redeem the refresh token,
	// so ChatGPT does not have to re-enter the passphrase after a redeploy.
	p2 := providerWithRefreshStore(t, path)
	g, ok := p2.store.consumeRefresh(refresh)
	if !ok {
		t.Fatal("refresh token must survive a provider restart when persisted")
	}
	if g.clientID != "client-1" || g.resource != p1.resource {
		t.Errorf("restored grant mismatch: %+v", g)
	}
	// Rotation still holds: after consuming once it is gone, in this and a new provider.
	if _, ok := p2.store.consumeRefresh(refresh); ok {
		t.Error("refresh must be single-use even after restart")
	}
	p3 := providerWithRefreshStore(t, path)
	if _, ok := p3.store.consumeRefresh(refresh); ok {
		t.Error("a consumed refresh must not reappear after another restart")
	}
}

func TestRefresh_AccessTokensNeverPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p1 := providerWithRefreshStore(t, path)
	access := p1.issueAccessToken("client-1", p1.resource, "mcp", time.Hour)
	p1.store.putRefresh(randToken(), refreshGrant{clientID: "client-1", scope: "mcp", resource: p1.resource, expiresAt: time.Now().Add(refreshTokenTTL)})

	// Access tokens are short-lived and must remain in memory only.
	p2 := providerWithRefreshStore(t, path)
	if p2.Authorize(bearerReq(access)) {
		t.Fatal("access tokens must never be persisted across restart")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read refresh store: %v", err)
	}
	if strings.Contains(string(body), access) {
		t.Fatalf("refresh store must not contain access tokens: %s", body)
	}
}

func TestRefresh_ExpiredNotRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p1 := providerWithRefreshStore(t, path)
	expired := randToken()
	p1.store.putRefresh(expired, refreshGrant{
		clientID:  "client-1",
		scope:     "mcp",
		resource:  p1.resource,
		expiresAt: time.Now().Add(-time.Hour), // already expired
	})
	p2 := providerWithRefreshStore(t, path)
	if _, ok := p2.store.consumeRefresh(expired); ok {
		t.Error("expired refresh tokens must not be restored")
	}
}

func TestRefresh_StoreFileMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not enforced on Windows; the deploy target is Linux")
	}
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p := providerWithRefreshStore(t, path)
	p.store.putRefresh(randToken(), refreshGrant{clientID: "c", scope: "mcp", resource: p.resource, expiresAt: time.Now().Add(time.Hour)})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("refresh store perm = %o, want 600", perm)
	}
}

func TestNewProvider_RejectsRelativeRefreshStorePath(t *testing.T) {
	_, err := NewProvider(Config{
		Issuer:           "https://h",
		Resource:         "https://h/mcp",
		Passphrase:       "x",
		RefreshStorePath: "relative.json",
	})
	if err == nil {
		t.Error("relative RefreshStorePath must be rejected")
	}
}
