package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPairingIsOneTimeExpiringAndDeviceCredentialIsIndependent(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, err := store.CreatePairing(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "wsl-development", pub)
	if err != nil {
		t.Fatal(err)
	}
	if device.ID == "" || device.Name != "wsl-development" || device.State != StateActive {
		t.Fatalf("device=%+v", device)
	}
	if _, err := store.Pair(code, "replay", pub); err == nil {
		t.Fatal("pairing code replay accepted")
	}

	expired, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := store.Pair(expired, "late", pub); err == nil {
		t.Fatal("expired pairing accepted")
	}
}

func TestSignedAuthenticationRejectsReplaySkewWrongKeyAndRevocation(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, _ := store.CreatePairing(10 * time.Minute)
	pub, private, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "wsl-development", pub)
	if err != nil {
		t.Fatal(err)
	}
	request := SignedRequest{DeviceID: device.ID, Timestamp: now.Unix(), Nonce: "nonce-0123456789abcdef", Method: "POST", Path: "/edge/v1/heartbeat", Body: []byte(`{"ready":true}`)}
	request.Signature = ed25519.Sign(private, request.Canonical())
	if _, err := store.Authenticate(request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(request); err == nil {
		t.Fatal("nonce replay accepted")
	}

	request.Nonce = "nonce-fedcba9876543210"
	request.Timestamp = now.Add(-3 * time.Minute).Unix()
	request.Signature = ed25519.Sign(private, request.Canonical())
	if _, err := store.Authenticate(request); err == nil {
		t.Fatal("stale signature accepted")
	}

	request.Timestamp = now.Unix()
	request.Nonce = "nonce-1111111111111111"
	_, wrong, _ := ed25519.GenerateKey(rand.Reader)
	request.Signature = ed25519.Sign(wrong, request.Canonical())
	if _, err := store.Authenticate(request); err == nil {
		t.Fatal("wrong device key accepted")
	}

	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	request.Nonce = "nonce-2222222222222222"
	request.Signature = ed25519.Sign(private, request.Canonical())
	if _, err := store.Authenticate(request); err == nil {
		t.Fatal("revoked device accepted")
	}
}

func TestIdentityStoreRejectsUnsafeInputs(t *testing.T) {
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, _ := store.CreatePairing(time.Minute)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, name := range []string{"", "../escape", "name with spaces"} {
		if _, err := store.Pair(code, name, pub); err == nil {
			t.Fatalf("name %q accepted", name)
		}
	}
}

func TestIdentityStoreRejectsSymlinkRootAndAncestor(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, root := range []string{link, filepath.Join(link, "nested")} {
		if store, err := Open(Config{Root: root}); err == nil {
			_ = store.Close()
			t.Fatalf("symlinked root %q accepted", root)
		}
	}
}
