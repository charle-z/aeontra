package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const testSandboxImageDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPrivateSandboxRunnerFailsClosedWithoutCompleteAuthority(t *testing.T) {
	for _, cfg := range []PrivateSandboxConfig{
		{},
		{URL: "http://127.0.0.1:9000", Token: strings.Repeat("x", 32), WorkspaceID: "primary"},
		{URL: "https://example.com", Token: strings.Repeat("x", 32), WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://8.8.8.8:9000", Token: strings.Repeat("x", 32), WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://127.0.0.1:9000/unexpected", Token: strings.Repeat("x", 32), WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://127.0.0.1:9000", Token: "short", WorkspaceID: "primary", ImageDigest: testSandboxImageDigest},
		{URL: "http://127.0.0.1:9000", Token: strings.Repeat("x", 32), WorkspaceID: "../host", ImageDigest: testSandboxImageDigest},
	} {
		r := NewPrivateSandboxRunner(cfg)
		if st := r.Status(context.Background()); st.Available || st.FreeTerminal {
			t.Fatalf("incomplete config reported available: %#v", st)
		}
	}
}

func TestPrivateSandboxRunnerRejectsRedirects(t *testing.T) {
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected++
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: source.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: t.TempDir(), ImageDigest: testSandboxImageDigest,
	})
	if status := r.Status(context.Background()); status.Available || redirected != 0 {
		t.Fatalf("private runner followed a redirect: status=%#v target_hits=%d", status, redirected)
	}
}

func TestPrivateSandboxRunnerRequiresMatchingAttestation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" || r.URL.Query().Get("profile_version") != sandboxprotocol.ProfileVersion {
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

func TestPrivateSandboxRunnerRejectsLegacyProfileWithoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" || r.URL.Query().Get("profile_version") != sandboxprotocol.ProfileVersion {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(privateSandboxStatus{
			Available: true, Backend: privateSandboxBackend, Rootless: true,
			NetworkProfile: "none", ImageDigest: testSandboxImageDigest,
			ProfileVersion: "l3-v1",
		})
	}))
	defer server.Close()

	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: t.TempDir(), ImageDigest: testSandboxImageDigest,
	})
	if status := r.Status(context.Background()); status.Available || status.FreeTerminal {
		t.Fatalf("legacy l3-v1 runner was used by the l3-v2 client: %#v", status)
	}
}

func TestPrivateSandboxRunnerSendsOnlyOpaqueWorkspaceAndRelativeDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "project", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	var got privateSandboxRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/status":
			if r.URL.Query().Get("profile_version") != sandboxprotocol.ProfileVersion {
				http.Error(w, "missing profile negotiation", http.StatusBadRequest)
				return
			}
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
		Dir: filepath.Join(root, "project", "sub"), Argv: []string{"bash", "-lc", "cargo test"},
		EnvAllowlist: map[string]string{"CI": "1"}, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "ok" || got.ProfileVersion != sandboxprotocol.ProfileVersion || got.WorkspaceID != "primary" || got.WorkspaceScope != "project" || got.RelativeDir != "sub" {
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

func TestPrivateSandboxRunnerRequiresWorkspaceSelectionAtMultiRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "one"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: "http://127.0.0.1:9", Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	_, err := r.Run(context.Background(), SandboxRunRequest{Dir: root, Argv: []string{"pwd"}})
	if err == nil || !strings.Contains(err.Error(), "cwd must select") {
		t.Fatalf("multi-repository root was not rejected before transport: %v", err)
	}
}

func TestPrivateSandboxRunnerAcceptsCasePreservingRepositoryScope(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "OpenAI-Codex")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	var got privateSandboxRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(privateSandboxResponse{IdempotencyKey: got.IdempotencyKey, RequestDigest: got.RequestDigest})
	}))
	defer server.Close()
	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	if _, err := r.Run(context.Background(), SandboxRunRequest{Dir: repository, Argv: []string{"pwd"}}); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceScope != "OpenAI-Codex" || got.RelativeDir != "" {
		t.Fatalf("case-preserving repository scope changed: %#v", got)
	}
}

func TestPrivateSandboxRunnerKeepsSubdirectoryInsideDirectRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	var got privateSandboxRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(privateSandboxResponse{IdempotencyKey: got.IdempotencyKey, RequestDigest: got.RequestDigest})
	}))
	defer server.Close()
	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	if _, err := r.Run(context.Background(), SandboxRunRequest{Dir: filepath.Join(root, "pkg", "sub"), Argv: []string{"pwd"}}); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceScope != "" || got.RelativeDir != "pkg/sub" {
		t.Fatalf("direct repository subdirectory was reinterpreted: %#v", got)
	}
}

func TestPrivateSandboxRunnerReturnsSemanticRemoteError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/run" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(sandboxprotocol.Error{Code: "workspace_secret_denied", Message: "selected workspace contains a policy-denied secret path"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	r := NewPrivateSandboxRunner(PrivateSandboxConfig{
		URL: server.URL, Token: strings.Repeat("x", 32), WorkspaceID: "primary",
		WorkspaceRoot: root, ImageDigest: testSandboxImageDigest,
	})
	_, err := r.Run(context.Background(), SandboxRunRequest{Dir: filepath.Join(root, "project"), Argv: []string{"pwd"}})
	if err == nil || !strings.Contains(err.Error(), "workspace_secret_denied") || strings.Contains(err.Error(), "400 Bad Request") {
		t.Fatalf("semantic executor error was not preserved: %v", err)
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
