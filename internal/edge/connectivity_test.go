package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

func TestDeviceConnectivityUsesAuthenticatedServerTimeAndSurvivesRestart(t *testing.T) {
	now := time.Date(2026, 10, 9, 17, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "edge")
	store, err := Open(Config{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := store.CreatePairing(time.Minute)
	pub, private, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", pub)
	if err != nil {
		t.Fatal(err)
	}
	assert := func(state string, last string) {
		t.Helper()
		got, err := store.DeviceConnectivity(device.ID)
		if err != nil || got.State != state || got.LastContactAt != last {
			t.Fatalf("connectivity=%+v err=%v", got, err)
		}
	}
	assert("unknown", "")
	req := SignedRequest{DeviceID: device.ID, Timestamp: now.Add(time.Minute).Unix(), Nonce: "nonce-connectivity-001", Method: "POST", Path: "/edge/v1/operations/lease", Body: []byte(`{}`)}
	req.Signature = ed25519.Sign(private, req.Canonical())
	if _, err := store.Authenticate(req); err != nil {
		t.Fatal(err)
	}
	last := now.Format(time.RFC3339)
	assert("recent_contact", last)
	now = now.Add(DeviceContactWindow)
	assert("recent_contact", last)
	now = now.Add(time.Second)
	assert("no_recent_contact", last)
	forged := req
	forged.Timestamp = now.Unix()
	forged.Nonce = "nonce-connectivity-forged"
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	forged.Signature = ed25519.Sign(wrongKey, forged.Canonical())
	if _, err := store.Authenticate(forged); err == nil {
		t.Fatal("forged contact accepted")
	}
	assert("no_recent_contact", last)
	now = now.Add(-91 * time.Second)
	now = now.Add(2 * time.Minute)
	if _, err := store.Authenticate(req); err == nil {
		t.Fatal("replay accepted")
	}
	assert("no_recent_contact", last)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(Config{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assert("no_recent_contact", last)
	now = now.Add(-3 * time.Minute)
	assert("unknown", last)
	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeviceConnectivity(device.ID); err == nil {
		t.Fatal("revoked device exposed")
	}
}
