package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPWorkspaceRegistrationUsesSignedStrictOpaqueSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 19, 18, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-workcell", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(workspaceRegistrationRequest{Workspaces: []WorkspaceRegistration{{
		WorkspaceID: "ws_55555555555555555555555555555555", Profile: "linux-workcell", Mode: "htb-linux",
	}}})
	response := performSignedRequest(t, NewHTTPHandler(store), device.ID, privateKey, now, "nonce-workspace-register-0001", http.MethodPost, "/edge/v1/workspaces/register", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "htb-linux") || strings.Contains(response.Body.String(), "ws_555") {
		t.Fatalf("response exposed registration body: %s", response.Body.String())
	}
	binding, err := store.ResolveWorkspace("ws_55555555555555555555555555555555")
	if err != nil || binding.DeviceID != device.ID {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}

	unknown := []byte(`{"workspaces":[],"target":"10.0.0.1"}`)
	rejected := performSignedRequest(t, NewHTTPHandler(store), device.ID, privateKey, now, "nonce-workspace-register-0002", http.MethodPost, "/edge/v1/workspaces/register", unknown)
	if rejected.Code != http.StatusBadRequest || strings.Contains(rejected.Body.String(), "10.0.0.1") {
		t.Fatalf("status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}
