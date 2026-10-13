//go:build linux

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/assets"
)

func TestAssetLibraryDisabledWithoutConfiguration(t *testing.T) {
	t.Setenv(assetLibraryEnv, "")
	library, err := buildAssetLibrary([]string{t.TempDir()})
	if library != nil || err != nil {
		t.Fatalf("library=%v err=%v", library, err)
	}
}

func TestAssetLibraryConfigurationFailsClosedAndSanitized(t *testing.T) {
	root := t.TempDir()
	secret := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	for _, path := range []string{"../" + secret, filepath.Join(root, secret)} {
		t.Setenv(assetLibraryEnv, path)
		if _, err := buildAssetLibrary([]string{root}); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestAssetLibraryLoadsReviewedPrivateSnapshotOutsideRoots(t *testing.T) {
	root := t.TempDir()
	private := t.TempDir()
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "library.json")
	entry := assets.Entry{ID: "example", Title: "Example logo", URL: "https://images.example.com/logo.png", SHA256: strings.Repeat("a", 64), MIME: "image/png", MaxBytes: 4096, License: "CC0-1.0", LicenseURL: "https://example.com/license", Attribution: "Example author", Provenance: "https://example.com/original", ReviewedBy: "operator", ReviewedAt: "2026-10-07"}
	encoded, _ := json.Marshal(map[string]any{"version": 1, "assets": []assets.Entry{entry}})
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(assetLibraryEnv, path)
	library, err := buildAssetLibrary([]string{root})
	if err != nil || library == nil {
		t.Fatalf("library=%v err=%v", library, err)
	}
	// Changing the file does not reload or change the accepted startup snapshot.
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := library.Entry("example"); err != nil || found != entry {
		t.Fatalf("snapshot=%+v err=%v", found, err)
	}
	if _, err := buildAssetLibrary([]string{root}); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildAssetLibrary([]string{root}); err == nil {
		t.Fatal("non-private manifest accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := buildAssetLibrary([]string{root}); err == nil {
		t.Fatal("non-private parent accepted")
	}
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := buildAssetLibrary([]string{private}); err == nil {
		t.Fatal("in-jail manifest accepted")
	}
	link := filepath.Join(private, "alias.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(assetLibraryEnv, link)
	if _, err := buildAssetLibrary([]string{root}); err == nil {
		t.Fatal("symlink manifest accepted")
	}
}
