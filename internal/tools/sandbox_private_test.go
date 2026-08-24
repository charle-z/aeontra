package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSandboxImageDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPrivateSandboxRunnerFailsClosedWithoutCompleteAuthority(t *testing.T) {
	for _, cfg := range []PrivateSandboxConfig{
		{},
		{URL: "http://127.0.0.1:9000", Token: strings.Repeat("x", 32), WorkspaceID: "primary"},
		{URL: "https://example.com", Token: strings.Repeat("x", 32), WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://127.0.0.1:9000", Token: "short", WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://127.0.0.1:9000", Token: strings.Repeat("x", 32), WorkspaceID: "../host", ImageDigest: testSandboxImageDigest},
	} {
		r := NewPrivateSandboxRunner(cfg)
		if st := r.Status(context.Background()); st.Available || st.FreeTerminal {
			t.Fatalf("incomplete config reported available: %#v", st)
		}
	}
}

func TestPrivateSandboxRunnerRequiresMatchingAttestation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(privateSandboxStatus{
			Available: true, Backend: privateSandboxBackend, Rootless: true,
			NetworkProfile: "none", ImageDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			ProfileVersion: privateSandboxProfileVersion,
		})
	}))
	defer server.Close()

	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary", ImageDigest: testSandboxImageDigest,
	})
	if st := r.Status(context.Background()); st.Available || st.FreeTerminal {
		t.Fatalf("mismatched image attestation reported available: %#v", st)
	}
}

func TestPrivateSandboxRunnerSendsOnlyOpaqueWorkspaceAndRelativeDirectory(t *testing.T) {
	root := t.TempDir()
	var got privateSandboxRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/status":
			_ = json.NewEncoder(w).Encode(privateSandboxStatus{
				Available: true, Backend: privateSandboxBackend, Rootless: true,
				NetworkProfile: "none", ImageDigest: testSandboxImageDigest,
				ProfileVersion: privateSandboxProfileVersion,
			})
		case "/v1/run":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(privateSandboxResponse{IdempotencyKey: got.IdempotencyKey, RequestDigest: got.RequestDigest, ExitCode: 0, Stdout: "ok", DurationMS: 7})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	res, err := r.Run(context.Background(), SandboxRunRequest{
		Dir: root + string(filepath.Separator) + "sub", Argv: []string{"bash", "-lc", "cargo test"},
		EnvAllowlist: map[string]string{"CI": "1"}, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "ok" || got.WorkspaceID != "primary" || got.RelativeDir != "sub" {
		t.Fatalf("request/result mismatch: got=%#v result=%#v", got, res)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), root) || got.NetworkProfile != "none" {
		t.Fatalf("host path or open network escaped into request: %s", encoded)
	}
	if got.RequestDigest == "" || got.IdempotencyKey == "" || got.OutputBytes <= 0 || got.MemoryMiB <= 0 || got.ProcessLimit <= 0 {
		t.Fatalf("bounded/durable request fields missing: %#v", got)
	}
}

func TestPrivateSandboxRunnerRejectsEscapingDirectory(t *testing.T) {
	root := t.TempDir()
	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: "http://127.0.0.1:9", Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	if _, err := r.Run(context.Background(), SandboxRunRequest{Dir: filepath.Join(filepath.Dir(root), "other"), Argv: []string{"true"}}); err == nil {
		t.Fatal("directory outside configured workspace should fail before transport")
	}
}
