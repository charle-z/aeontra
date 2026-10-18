package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryRegistryRejectsReplacedManifest(t *testing.T) {
	registry, root := newRegistryFixture(t, "repo")
	manifest := filepath.Join(root, "repo", "package.json")
	if err := os.Rename(manifest, manifest+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.lookup("repo"); err == nil {
		t.Fatal("replacement manifest accepted")
	}
}
