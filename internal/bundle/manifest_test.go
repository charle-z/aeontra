package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSignedManifestVerifiesCompleteIndivisibleBundle(t *testing.T) {
	root := t.TempDir()
	paths := DefaultLayout()
	for component, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("p15:"+component), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{
		Version: 1, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Components: map[string]string{},
	}
	for component, relative := range paths {
		digest, err := HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		manifest.Components[component] = digest
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(root, manifest, signature, publicKey, paths, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit,
		ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash:     manifest.CatalogHash,
		Architecture:    "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Release != manifest.Release || got.Commit != manifest.Commit {
		t.Fatalf("unexpected verified bundle: %+v", got)
	}
}

func TestBundleVerificationFailsBeforeRuntimeWithPreciseSafeCodes(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base := Manifest{
		Version: 1, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Components: map[string]string{},
	}
	root := t.TempDir()
	paths := map[string]string{}
	for _, component := range RequiredComponents() {
		paths[component] = component
		path := filepath.Join(root, component)
		if err := os.WriteFile(path, []byte(component), 0o600); err != nil {
			t.Fatal(err)
		}
		digest, err := HashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		base.Components[component] = digest
	}
	signature, err := Sign(base, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		missing string
		want    Code
	}{
		{name: "provider missing", missing: ComponentProvider, want: ProviderOutdated},
		{name: "driver missing", missing: ComponentDriver, want: DriverOutdated},
		{name: "bundle incomplete", missing: ComponentEdge, want: BundleMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidatePaths := make(map[string]string, len(paths))
			for component, path := range paths {
				candidatePaths[component] = path
			}
			delete(candidatePaths, test.missing)
			_, err := Verify(root, base, signature, publicKey, candidatePaths, Compatibility{
				Release: base.Release, Commit: base.Commit,
				ProtocolVersion: base.ProtocolVersion, CatalogHash: base.CatalogHash, Architecture: base.Architecture,
			})
			var verificationError *VerificationError
			if !errors.As(err, &verificationError) || verificationError.Code != test.want {
				t.Fatalf("got %v, want code %s", err, test.want)
			}
		})
	}
}

func TestBundleVerificationRejectsTamperingAndIncompatibleCatalog(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Version: 1, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Components: map[string]string{},
	}
	for _, component := range RequiredComponents() {
		manifest.Components[component] = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}
	signature, err := Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	tampered := manifest
	tampered.Release = "p15.0.1"
	_, err = Verify(t.TempDir(), tampered, signature, publicKey, map[string]string{}, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit,
		ProtocolVersion: manifest.ProtocolVersion, CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture,
	})
	assertBundleCode(t, err, ManifestInvalid)

	_, err = Verify(t.TempDir(), manifest, signature, publicKey, map[string]string{}, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit,
		ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash:     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Architecture:    manifest.Architecture,
	})
	assertBundleCode(t, err, BundleMismatch)
}

func assertBundleCode(t *testing.T, err error, want Code) {
	t.Helper()
	var verificationError *VerificationError
	if !errors.As(err, &verificationError) || verificationError.Code != want {
		t.Fatalf("got %v, want code %s", err, want)
	}
}
