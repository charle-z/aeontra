package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestReleaseChannelHasCanonicalSignedClosedIdentity(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	channel := Channel{
		Version: 1, Release: "p15.3.0", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProtocolVersion: "mcp-devbox.edge-bundle.v1",
		CatalogHash:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Architecture:    "amd64",
		ArchiveHash:     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	canonical, signature, err := SignChannel(channel, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, canonical, signature) {
		t.Fatal("channel signature did not verify")
	}
	tampered := append([]byte(nil), canonical...)
	tampered[len(tampered)-1] ^= 1
	if ed25519.Verify(publicKey, tampered, signature) {
		t.Fatal("tampered channel signature verified")
	}
}

func TestWindowsReleaseChannelRequiresPlatformBoundVersion(t *testing.T) {
	channel := Channel{
		Version: 2, Release: "v1.2.0", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProtocolVersion: "mcp-devbox.edge-bundle.v1",
		CatalogHash:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Architecture:    "amd64", Platform: "windows",
		ArchiveHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	if _, err := CanonicalChannel(channel); err != nil {
		t.Fatal(err)
	}
	channel.Platform = ""
	if _, err := CanonicalChannel(channel); err == nil {
		t.Fatal("version two channel accepted without a Windows platform binding")
	}
	channel.Version = 1
	channel.Platform = "windows"
	if _, err := CanonicalChannel(channel); err == nil {
		t.Fatal("legacy channel accepted a platform field")
	}
}
