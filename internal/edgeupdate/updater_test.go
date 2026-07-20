//go:build !windows

package edgeupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/bundle"
)

type officialRoundTripper struct{ channel, signature []byte }

func (r officialRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	body := r.channel
	if filepath.Ext(request.URL.Path) == ".sig" {
		body = r.signature
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
}

func TestStableAvailableUsesOnlySignedOfficialChannel(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	channel := bundle.Channel{Version: 1, Release: "p15.9.0", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtocolVersion: "mcp-devbox.edge-bundle.v1", CatalogHash: "sha256:" + string(bytes.Repeat([]byte{'b'}, 64)), Architecture: runtime.GOARCH, ArchiveHash: "sha256:" + string(bytes.Repeat([]byte{'c'}, 64))}
	body, signature, err := bundle.SignChannel(channel, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	resolver := OfficialResolver{PublicKey: publicKey, Client: &http.Client{Transport: officialRoundTripper{channel: body, signature: signature}}}
	available, err := resolver.StableAvailable(context.Background(), "p15.8.0")
	if err != nil || !available {
		t.Fatalf("available=%t err=%v", available, err)
	}
	available, err = resolver.StableAvailable(context.Background(), "p15.9.0")
	if err != nil || available {
		t.Fatalf("current available=%t err=%v", available, err)
	}
	resolver.Client = &http.Client{Transport: officialRoundTripper{channel: body, signature: bytes.Repeat([]byte{0}, ed25519.SignatureSize)}}
	if _, err := resolver.StableAvailable(context.Background(), "p15.8.0"); err == nil {
		t.Fatal("tampered channel accepted")
	}
}

type fakeService struct {
	restarts int
	healthy  bool
}

func (s *fakeService) InstallUnit(string) error { return nil }
func (s *fakeService) RestartEdge() error       { s.restarts++; return nil }
func (s *fakeService) EdgeHealthy() bool        { return s.healthy }

func TestUpdaterInstallsAtomicallyIdempotentlyAndRollsBack(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeService{healthy: true}
	engine := Engine{Root: root, PublicKey: publicKey, Service: service}

	firstSource, firstCompatibility := signedRelease(t, "p15.0.0", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", privateKey)
	if status, err := engine.Install(firstSource, firstCompatibility); err != nil || status.Release != "p15.0.0" || status.PreviousRelease != "" {
		t.Fatalf("first install = %+v, %v", status, err)
	}
	assertCurrentRelease(t, root, "p15.0.0")

	if status, err := engine.Install(firstSource, firstCompatibility); err != nil || status.Release != "p15.0.0" || service.restarts != 1 {
		t.Fatalf("idempotent install = %+v, %v, restarts=%d", status, err, service.restarts)
	}

	secondSource, secondCompatibility := signedRelease(t, "p15.0.1", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", privateKey)
	if status, err := engine.Install(secondSource, secondCompatibility); err != nil || status.Release != "p15.0.1" || status.PreviousRelease != "p15.0.0" {
		t.Fatalf("upgrade = %+v, %v", status, err)
	}
	assertCurrentRelease(t, root, "p15.0.1")

	if status, err := engine.Rollback(); err != nil || status.Release != "p15.0.0" || status.PreviousRelease != "p15.0.1" {
		t.Fatalf("rollback = %+v, %v", status, err)
	}
	assertCurrentRelease(t, root, "p15.0.0")
}

func TestUpdaterRestoresPreviousReleaseWhenHealthCheckFails(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeService{healthy: true}
	engine := Engine{Root: root, PublicKey: publicKey, Service: service}
	firstSource, firstCompatibility := signedRelease(t, "p15.1.0", "cccccccccccccccccccccccccccccccccccccccc", privateKey)
	if _, err := engine.Install(firstSource, firstCompatibility); err != nil {
		t.Fatal(err)
	}
	service.healthy = false
	secondSource, secondCompatibility := signedRelease(t, "p15.1.1", "dddddddddddddddddddddddddddddddddddddddd", privateKey)
	if _, err := engine.Install(secondSource, secondCompatibility); !errors.Is(err, ErrHealthCheck) {
		t.Fatalf("got %v, want health failure", err)
	}
	assertCurrentRelease(t, root, "p15.1.0")
}

func TestUpdaterRejectsUnsignedOrCallerMixedBundleBeforeActivation(t *testing.T) {
	root := t.TempDir()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeService{healthy: true}
	engine := Engine{Root: root, PublicKey: publicKey, Service: service}
	source, compatibility := signedRelease(t, "p15.2.0", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", privateKey)
	if err := os.WriteFile(filepath.Join(source, "opencode-provider", "index.js"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Install(source, compatibility); err == nil {
		t.Fatal("tampered bundle was activated")
	}
	if _, err := os.Lstat(filepath.Join(root, CurrentLink)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current link exists after rejected install: %v", err)
	}
}

func signedRelease(t *testing.T, release, commit string, privateKey ed25519.PrivateKey) (string, bundle.Compatibility) {
	t.Helper()
	root := t.TempDir()
	for component, relative := range bundle.DefaultLayout() {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(release+":"+component), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := bundle.Metadata{
		Release: release, Commit: commit, ProtocolVersion: "mcp-devbox.edge-bundle.v1",
		CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Architecture: "amd64",
	}
	manifest, err := bundle.Build(root, metadata)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := bundle.Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bundle.ManifestFile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, bundle.SignatureFile), signature, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, bundle.Compatibility{
		Release: release, Commit: commit, ProtocolVersion: metadata.ProtocolVersion,
		CatalogHash: metadata.CatalogHash, Architecture: metadata.Architecture,
	}
}

func assertCurrentRelease(t *testing.T, root, want string) {
	t.Helper()
	target, err := os.Readlink(filepath.Join(root, CurrentLink))
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(ReleasesDirectory, want) {
		t.Fatalf("current target = %q, want %q", target, filepath.Join(ReleasesDirectory, want))
	}
}
