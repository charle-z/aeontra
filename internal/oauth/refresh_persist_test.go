package oauth

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func providerWithTokenStores(t *testing.T, accessPath, refreshPath string) *Provider {
	t.Helper()
	p, err := NewProvider(Config{
		Issuer:           "https://mcp.example.com",
		Resource:         "https://mcp.example.com/mcp",
		Passphrase:       "correct horse",
		AccessStorePath:  accessPath,
		RefreshStorePath: refreshPath,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

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
	g, ok, err := p2.store.consumeRefresh(refresh)
	if err != nil || !ok {
		t.Fatal("refresh token must survive a provider restart when persisted")
	}
	if g.clientID != "client-1" || g.resource != p1.resource {
		t.Errorf("restored grant mismatch: %+v", g)
	}
	// Rotation still holds: after consuming once it is gone, in this and a new provider.
	if _, ok, _ := p2.store.consumeRefresh(refresh); ok {
		t.Error("refresh must be single-use even after restart")
	}
	p3 := providerWithRefreshStore(t, path)
	if _, ok, _ := p3.store.consumeRefresh(refresh); ok {
		t.Error("a consumed refresh must not reappear after another restart")
	}
}

func TestRefreshStoreNeverContainsRawAccessTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p1 := providerWithRefreshStore(t, path)
	access := p1.issueAccessToken("client-1", p1.resource, "mcp", time.Hour)
	p1.store.putRefresh(randToken(), refreshGrant{clientID: "client-1", scope: "mcp", resource: p1.resource, expiresAt: time.Now().Add(refreshTokenTTL)})

	// A refresh-only store must never contain or restore a raw access bearer.
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
	if _, ok, _ := p2.store.consumeRefresh(expired); ok {
		t.Error("expired refresh tokens must not be restored")
	}
}

func TestRefreshConsumptionFailsClosedWhenRotationCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth-refresh.json")
	p := providerWithRefreshStore(t, path)
	refresh := randToken()
	if err := p.store.putRefresh(refresh, refreshGrant{clientID: "client-1", scope: "mcp", resource: p.resource, expiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// An existing directory cannot be replaced by the atomic store rename. The
	// failed rotation must not return a usable grant or drop the in-memory token.
	p.store.mu.Lock()
	p.store.refreshStorePath = t.TempDir()
	p.store.mu.Unlock()
	if _, ok, err := p.store.consumeRefresh(refresh); err == nil || ok {
		t.Fatalf("non-durable rotation accepted: ok=%t err=%v", ok, err)
	}
	p.store.mu.Lock()
	_, retained := p.store.refresh[refresh]
	p.store.mu.Unlock()
	if !retained {
		t.Fatal("failed durable rotation removed the only valid in-memory grant")
	}
}

func TestRefreshRotationRollsBackAccessWhenRefreshPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	accessPath := filepath.Join(dir, "oauth-access.json")
	refreshPath := filepath.Join(dir, "oauth-refresh.json")
	p := providerWithTokenStores(t, accessPath, refreshPath)
	old := randToken()
	grant := refreshGrant{clientID: "client-1", scope: "mcp", resource: p.resource, expiresAt: time.Now().Add(time.Hour)}
	if err := p.store.putRefresh(old, grant); err != nil {
		t.Fatal(err)
	}

	p.store.mu.Lock()
	p.store.refreshStorePath = t.TempDir()
	p.store.mu.Unlock()
	rec := postToken(t, p, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	p.store.mu.Lock()
	_, retained := p.store.refresh[old]
	accessCount := len(p.store.access)
	p.store.mu.Unlock()
	if !retained {
		t.Fatal("failed refresh persistence consumed the old refresh token")
	}
	if accessCount != 0 {
		t.Fatalf("failed rotation left %d access grants, want 0", accessCount)
	}

	p.store.mu.Lock()
	p.store.refreshStorePath = refreshPath
	p.store.mu.Unlock()
	rec = postToken(t, p, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}})
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRefreshRotationDoesNotConsumeOldTokenWhenAccessPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	accessPath := filepath.Join(dir, "oauth-access.json")
	refreshPath := filepath.Join(dir, "oauth-refresh.json")
	p := providerWithTokenStores(t, accessPath, refreshPath)
	old := randToken()
	grant := refreshGrant{clientID: "client-1", scope: "mcp", resource: p.resource, expiresAt: time.Now().Add(time.Hour)}
	if err := p.store.putRefresh(old, grant); err != nil {
		t.Fatal(err)
	}

	p.store.mu.Lock()
	p.store.accessStorePath = t.TempDir()
	p.store.mu.Unlock()
	rec := postToken(t, p, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	p.store.mu.Lock()
	_, retained := p.store.refresh[old]
	p.store.mu.Unlock()
	if !retained {
		t.Fatal("failed access persistence consumed the old refresh token")
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

func TestNewProviderRejectsAliasedPersistenceStores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oauth.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"grants":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{Issuer: "https://h", Resource: "https://h/mcp", Passphrase: "x", AccessStorePath: path, RefreshStorePath: path}); err == nil {
		t.Fatal("one file was accepted for two OAuth store formats")
	}
	if runtime.GOOS == "windows" {
		return
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{Issuer: "https://h", Resource: "https://h/mcp", Passphrase: "x", ClientStorePath: path, RefreshStorePath: filepath.Join(alias, "oauth.json")}); err == nil {
		t.Fatal("symlink-aliased OAuth stores were accepted")
	}
	fileAlias := filepath.Join(t.TempDir(), "oauth-link.json")
	if err := os.Symlink(path, fileAlias); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{Issuer: "https://h", Resource: "https://h/mcp", Passphrase: "x", AccessStorePath: path, RefreshStorePath: fileAlias}); err == nil {
		t.Fatal("file-symlink-aliased OAuth stores were accepted")
	}
	hardLink := filepath.Join(t.TempDir(), "oauth-hardlink.json")
	if err := os.Link(path, hardLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(Config{Issuer: "https://h", Resource: "https://h/mcp", Passphrase: "x", AccessStorePath: path, RefreshStorePath: hardLink}); err == nil {
		t.Fatal("hard-linked OAuth stores were accepted")
	}
}
