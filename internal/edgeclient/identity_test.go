package edgeclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestPairPersistsDeviceKeyPrivatelyAndCanSignRequests(t *testing.T) {
	edgeStore, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer edgeStore.Close()
	code, _ := edgeStore.CreatePairing(time.Minute)
	server := httptest.NewTLSServer(edge.NewHTTPHandler(edgeStore))
	defer server.Close()

	stateRoot := filepath.Join(t.TempDir(), "client-state")
	identity, err := Pair(context.Background(), PairOptions{
		ServerURL:  server.URL,
		Code:       code,
		Name:       "wsl-development",
		StateRoot:  stateRoot,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if identity.DeviceID == "" || identity.ServerURL != server.URL {
		t.Fatalf("identity=%+v", identity)
	}
	for _, path := range []string{stateRoot, filepath.Join(stateRoot, identityFile), filepath.Join(stateRoot, privateKeyFile)} {
		assertPrivateIdentityPath(t, path)
	}
	loaded, key, err := LoadIdentity(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != identity || len(key) != ed25519.PrivateKeySize {
		t.Fatalf("loaded=%+v key-size=%d", loaded, len(key))
	}
}

func TestPairRejectsInsecureEndpointAndUnsafeStateRoot(t *testing.T) {
	if _, err := Pair(context.Background(), PairOptions{ServerURL: "http://example.com", Code: "ep_test", Name: "wsl-development", StateRoot: filepath.Join(t.TempDir(), "state")}); err == nil {
		t.Fatal("insecure public endpoint accepted")
	}
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Pair(context.Background(), PairOptions{ServerURL: "https://example.com", Code: "ep_test", Name: "wsl-development", StateRoot: link}); err == nil {
		t.Fatal("symlink state root accepted")
	}
}

func TestLoadIdentityRejectsPermissivePrivateKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, identityFile), []byte(`{"schema_version":1,"server_url":"https://example.com","device_id":"ed_0123456789abcdef0123456789abcdef","name":"wsl-development"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, privateKeyFile), []byte("invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadIdentity(root); err == nil {
		t.Fatal("permissive private key accepted")
	}
}

func TestLoadIdentityRejectsMalformedDeviceIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, identityFile), []byte(`{"schema_version":1,"server_url":"https://example.com","device_id":"ed_not-opaque","name":"wsl-development"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, ed25519.PrivateKeySize)
	if err := os.WriteFile(filepath.Join(root, privateKeyFile), []byte(base64.RawURLEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadIdentity(root); err == nil {
		t.Fatal("malformed device identity accepted")
	}
}
