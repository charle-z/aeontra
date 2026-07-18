//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeLinuxWorkcellReadonlyDirectoryAcceptsOwnedReadonlyDirectoryInsideRoot(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "wordlists")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, ok := safeLinuxWorkcellReadonlyDirectory(directory, root, os.Geteuid())
	if !ok || resolved != directory {
		t.Fatalf("resolved=%q ok=%t", resolved, ok)
	}
}

func TestSafeLinuxWorkcellReadonlyDirectoryAllowsAbsentOptionalDirectory(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "absent")
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(missing, root, os.Geteuid()); ok || resolved != "" {
		t.Fatalf("absent directory accepted: %q", resolved)
	}
}

func TestSafeLinuxWorkcellReadonlyDirectoryRejectsWritableDirectory(t *testing.T) {
	root := t.TempDir()
	writable := filepath.Join(root, "writable")
	if err := os.Mkdir(writable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o775); err != nil {
		t.Fatal(err)
	}
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(writable, root, os.Geteuid()); ok || resolved != "" {
		t.Fatalf("writable directory accepted: %q", resolved)
	}
}

func TestSafeLinuxWorkcellReadonlyDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "wordlists")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(link, root, os.Geteuid()); ok || resolved != "" {
		t.Fatalf("symlink accepted: %q", resolved)
	}
}

func TestSafeLinuxWorkcellReadonlyDirectoryRejectsWrongOwner(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "wordlists")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	wrongOwner := os.Geteuid() + 1
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(directory, root, wrongOwner); ok || resolved != "" {
		t.Fatalf("wrong owner accepted: %q", resolved)
	}
}

func TestSafeLinuxWorkcellReadonlyDirectoryRejectsRootAndOutside(t *testing.T) {
	root := t.TempDir()
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(root, root, os.Geteuid()); ok || resolved != "" {
		t.Fatalf("root accepted: %q", resolved)
	}
	outside := t.TempDir()
	if resolved, ok := safeLinuxWorkcellReadonlyDirectory(outside, root, os.Geteuid()); ok || resolved != "" {
		t.Fatalf("outside directory accepted: %q", resolved)
	}
}
