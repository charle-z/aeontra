package bundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseNameAcceptsLegacyBridgeAndStableSemanticVersions(t *testing.T) {
	for _, release := range []string{"p15.0.45", "v0.1.0", "v1.0.0", "v12.34.56"} {
		if !ValidRelease(release) {
			t.Errorf("release %q was rejected", release)
		}
	}
	for _, release := range []string{
		"", "stable", "1.0.0", "edge-v1.0.0", "v1.0", "v1.0.0-rc.1",
		"v1.0.0+build", "v01.0.0", "v1.00.0", "v1.0.00", "../v1.0.0",
	} {
		if ValidRelease(release) {
			t.Errorf("invalid release %q was accepted", release)
		}
	}
}

func TestSignedManifestVerifiesCompleteIndivisibleBundle(t *testing.T) {
	root := t.TempDir()
	paths, ok := layoutForVersion(3)
	if !ok {
		t.Fatal("version three layout unavailable")
	}
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
		Version: 3, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
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

func TestLegacyVersionOneBundleRemainsVerifiableForRollback(t *testing.T) {
	root := t.TempDir()
	paths, ok := layoutForVersion(1)
	if !ok {
		t.Fatal("legacy layout unavailable")
	}
	manifest := Manifest{
		Version: 1, Release: "p15.0.4", Commit: "3a91fb703ca8543869098ba0aa8c80f69e8233a1",
		ProtocolVersion: "mcp-devbox.edge-bundle.v1", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Components: map[string]string{},
	}
	for component, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(component), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest.Components[component], _ = HashFile(path)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(root, manifest, signature, publicKey, paths, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit, ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVersionTwoBundleWithoutBundledGitHubCLIRemainsVerifiableForRollback(t *testing.T) {
	paths, ok := layoutForVersion(2)
	if !ok {
		t.Fatal("version two layout unavailable")
	}
	if _, exists := paths[ComponentGitHubCLI]; exists {
		t.Fatal("version two unexpectedly requires bundled GitHub CLI")
	}
}

func TestVersionThreeWithBundledGitHubCLIRemainsVerifiableForRollback(t *testing.T) {
	root := t.TempDir()
	layout, ok := layoutForVersion(3)
	if !ok {
		t.Fatal("version three layout unavailable")
	}
	if _, exists := layout[ComponentGitHubCLI]; !exists {
		t.Fatal("version three manifest is missing GitHub CLI")
	}
	if _, exists := layout[ComponentCodex]; exists {
		t.Fatal("version three unexpectedly requires Codex")
	}
	for component, relative := range layout {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(component), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := BuildVersion(root, Metadata{
		Release: "p15.0.35", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64",
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 3 || len(manifest.Components) != len(versionThreeRequiredComponents()) {
		t.Fatalf("bridge manifest=%+v", manifest)
	}
}

func TestBuildEmitsVersionFiveWithOnlyPinnedCodexHarness(t *testing.T) {
	root := t.TempDir()
	layout, ok := layoutForVersion(5)
	if !ok {
		t.Fatal("version five layout unavailable")
	}
	for component, relative := range layout {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(component), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := Build(root, Metadata{
		Release: "p15.0.35", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 5 {
		t.Fatalf("version=%d, want version 5", manifest.Version)
	}
	for _, component := range []string{ComponentCodex, ComponentCodexPin} {
		if _, exists := manifest.Components[component]; !exists {
			t.Fatalf("version five manifest is missing %s", component)
		}
	}
	for _, component := range []string{
		ComponentDriver, ComponentNode, ComponentProvider, ComponentHTBActions,
		ComponentDevActions, ComponentProviderPackage, ComponentOpenCode, ComponentOpenCodeLock,
	} {
		if _, exists := manifest.Components[component]; exists {
			t.Fatalf("Codex-only manifest unexpectedly contains %s", component)
		}
	}
	if got := layout[ComponentSystemd]; got != "systemd/mcp-devbox-edge@.service" {
		t.Fatalf("systemd layout=%q, want neutral Edge unit", got)
	}
}

func TestVersionSixWindowsBundleBindsPlatformAndClosedLayout(t *testing.T) {
	root := t.TempDir()
	layout := WindowsLayout()
	for component, relative := range layout {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(component), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := BuildVersion(root, Metadata{
		Release: "v1.2.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "mcp-devbox.edge-bundle.v1", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Platform: "windows",
	}, 6)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(manifest, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(root, manifest, signature, publicKey, layout, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit, ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture, Platform: "windows",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(root, manifest, signature, publicKey, layout, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit, ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture,
	}); err == nil {
		t.Fatal("Windows bundle verified without an exact platform binding")
	}
	if _, err := BuildVersion(root, Metadata{
		Release: manifest.Release, Commit: manifest.Commit, ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture,
	}, 6); err == nil {
		t.Fatal("version six accepted a platform-free bundle")
	}
}

func TestVersionFourHybridBundleRemainsVerifiableForRollback(t *testing.T) {
	layout, ok := layoutForVersion(4)
	if !ok {
		t.Fatal("version four layout unavailable")
	}
	for _, component := range []string{ComponentOpenCode, ComponentOpenCodeLock, ComponentCodex, ComponentCodexPin} {
		if _, exists := layout[component]; !exists {
			t.Fatalf("version four rollback layout is missing %s", component)
		}
	}
	if got := layout[ComponentSystemd]; got != "systemd/mcp-devbox-opencode-edge@.service" {
		t.Fatalf("version four systemd layout=%q", got)
	}
}

func TestBundleVerificationFailsBeforeRuntimeWithPreciseSafeCodes(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base := Manifest{
		Version: 4, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
		ProtocolVersion: "2025-06-18", CatalogHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Architecture: "amd64", Components: map[string]string{},
	}
	root := t.TempDir()
	paths := map[string]string{}
	for _, component := range versionFourRequiredComponents() {
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
		Version: CurrentManifestVersion, Release: "p15.0.0", Commit: "54891fe7bced14e5eacace754f0072ad4d7996c2",
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
