package edgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestRegisterWorkspacesSendsOnlyOpaqueProfileAndMode(t *testing.T) {
	store, err := edge.Open(edge.Config{Root: filepath.Join(t.TempDir(), "server-edge")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	code, _ := store.CreatePairing(time.Minute)
	base := edge.NewHTTPHandler(store)
	var captured []byte
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == workspaceRegistrationPath {
			captured, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(captured))
		}
		base.ServeHTTP(w, r)
	}))
	defer server.Close()
	stateRoot := filepath.Join(t.TempDir(), "client")
	identity, err := Pair(context.Background(), PairOptions{ServerURL: server.URL, Code: code, Name: "parrot-workcell", StateRoot: stateRoot, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewTransport(stateRoot, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{
		ID: "ws_66666666666666666666666666666666", Path: "/home/charles/htb-machines/Sensitive",
		Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeHTBLinux,
		MachineName: "Sensitive", TargetIP: "10.129.1.2", VPNInterface: "tun0",
	}
	if err := transport.RegisterWorkspaces(context.Background(), []Workspace{workspace}); err != nil {
		t.Fatal(err)
	}
	text := string(captured)
	for _, forbidden := range []string{workspace.Path, workspace.MachineName, workspace.TargetIP, workspace.VPNInterface, "machine_name", "target_ip", "path", "vpn_interface"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("registration exposed %q: %s", forbidden, text)
		}
	}
	var payload struct {
		Workspaces []edge.WorkspaceRegistration `json:"workspaces"`
	}
	if err := json.Unmarshal(captured, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Workspaces) != 1 || payload.Workspaces[0].WorkspaceID != workspace.ID || payload.Workspaces[0].Profile != "linux-workcell" || payload.Workspaces[0].Mode != "htb-linux" {
		t.Fatalf("payload=%+v", payload)
	}
	binding, err := store.ResolveWorkspace(workspace.ID)
	if err != nil || binding.DeviceID != identity.DeviceID {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
}
