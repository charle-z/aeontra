package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStableOwnedRegularRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if file, _, err := openStableOwnedRegular(link); err == nil {
		_ = file.Close()
		t.Fatal("symlink should be rejected")
	}
}

func TestOpenStableOwnedRegularUnderRejectsIntermediateSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "github.json")
	if err := os.WriteFile(secret, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "artifacts")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if file, _, err := openStableOwnedRegularUnder(root, filepath.Join(link, "github.json")); err == nil {
		_ = file.Close()
		t.Fatal("intermediate symlink escaped the managed root")
	}
}
