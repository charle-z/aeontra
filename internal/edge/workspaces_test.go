package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func pairWorkspaceTestDevice(t *testing.T, store *Store, name string) Device {
	t.Helper()
	code, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.Pair(code, name, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestWorkspaceRegistrationResolvesOnlyRecognizedActiveBindings(t *testing.T) {
	store := openHTTPTestStore(t)
	device := pairWorkspaceTestDevice(t, store, "parrot-workcell")
	devID := "ws_11111111111111111111111111111111"
	htbID := "ws_22222222222222222222222222222222"
	windowsID := "ws_33333333333333333333333333333333"
	status, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{
		{WorkspaceID: devID, Profile: "linux-workcell", Mode: "dev"},
		{WorkspaceID: htbID, Profile: "linux-workcell", Mode: "htb-linux"},
		{WorkspaceID: windowsID, Profile: "windows-workcell", Mode: "dev"},
	})
	if err != nil || status.Count != 3 || status.UpdatedAt.IsZero() {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	binding, err := store.ResolveWorkspace(htbID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.DeviceID != device.ID || binding.Profile != "linux-workcell" || binding.Mode != "htb-linux" || binding.UpdatedAt.IsZero() {
		t.Fatalf("binding=%+v", binding)
	}
	binding, err = store.ResolveWorkspace(windowsID)
	if err != nil || binding.DeviceID != device.ID || binding.Profile != "windows-workcell" || binding.Mode != "dev" {
		t.Fatalf("Windows binding=%+v err=%v", binding, err)
	}
	if _, err := store.ResolveWorkspace("ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing workspace error=%v", err)
	}
	if err := store.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveWorkspace(htbID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("revoked workspace error=%v", err)
	}
}

func TestWorkspaceRegistrationReplacesSnapshotAndRejectsCrossDeviceOrModeConfusion(t *testing.T) {
	store := openHTTPTestStore(t)
	first := pairWorkspaceTestDevice(t, store, "parrot-first")
	second := pairWorkspaceTestDevice(t, store, "parrot-second")
	workspaceID := "ws_44444444444444444444444444444444"
	if _, err := store.RegisterWorkspaces(first.ID, []WorkspaceRegistration{{WorkspaceID: workspaceID, Profile: "sandbox", Mode: "dev"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterWorkspaces(second.ID, []WorkspaceRegistration{{WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "dev"}}); err == nil || !strings.Contains(err.Error(), "another edge device") {
		t.Fatalf("cross-device error=%v", err)
	}
	for _, item := range []WorkspaceRegistration{
		{WorkspaceID: workspaceID, Profile: "sandbox", Mode: "htb-linux"},
		{WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "unknown"},
		{WorkspaceID: workspaceID, Profile: "windows-workcell", Mode: "htb-linux"},
		{WorkspaceID: workspaceID, Profile: "unknown", Mode: "dev"},
	} {
		if _, err := store.RegisterWorkspaces(first.ID, []WorkspaceRegistration{item}); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("item=%+v err=%v", item, err)
		}
	}
	if _, err := store.RegisterWorkspaces(first.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveWorkspace(workspaceID); err == nil {
		t.Fatal("stale workspace survived replacement snapshot")
	}
}
