package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStableRegularRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if file, _, err := openContainedRegular(root, link); err == nil {
		_ = file.Close()
		t.Fatal("symlink should be rejected")
	}
}

func TestOpenStableRegularReturnsOpenedIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, info, err := openContainedRegular(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		t.Fatalf("opened identity mismatch: info=%v opened=%v err=%v", info, opened, err)
	}
}

func TestOpenContainedRegularRejectsIntermediateSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "artifacts")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if file, _, err := openContainedRegular(root, filepath.Join(link, "secret.txt")); err == nil {
		_ = file.Close()
		t.Fatal("intermediate symlink escaped the repository root")
	}
}
