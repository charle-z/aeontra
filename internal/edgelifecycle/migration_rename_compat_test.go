//go:build !windows

package edgelifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRenameNoReplaceFallsBackWhenFilesystemDoesNotSupportFlag(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	destination := filepath.Join(root, "preferred")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "state.db"), []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := renameNoReplaceWith(source, destination, func(_, _ string) error {
		called = true
		return unix.EOPNOTSUPP
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("atomic no-replace operation was not attempted")
	}
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remained after fallback rename: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "state.db"))
	if err != nil || string(content) != "preserved" {
		t.Fatalf("destination content=%q err=%v", content, err)
	}
}

func TestRenameNoReplaceFallbackNeverOverwritesExistingDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "legacy")
	destination := filepath.Join(root, "preferred")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "existing"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := renameNoReplaceWith(source, destination, func(_, _ string) error { return unix.EINVAL })
	if err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination error=%v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source was changed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "existing"))
	if err != nil || string(content) != "keep" {
		t.Fatalf("existing destination changed: content=%q err=%v", content, err)
	}
}

func TestRenameNoReplaceDoesNotFallbackForPermissionOrCrossDeviceErrors(t *testing.T) {
	for _, failure := range []error{unix.EPERM, unix.EACCES, unix.EXDEV} {
		t.Run(failure.Error(), func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "legacy")
			destination := filepath.Join(root, "preferred")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			err := renameNoReplaceWith(source, destination, func(_, _ string) error { return failure })
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v want=%v", err, failure)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatalf("source changed after hard failure: %v", err)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("destination appeared after hard failure: %v", err)
			}
		})
	}
}

func TestPortableRenamePlaceholderIsRemovedWhenSourceRenameFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "missing")
	destination := filepath.Join(root, "preferred")
	if err := portableDirectoryRenameNoReplace(source, destination); err == nil {
		t.Fatal("missing source was accepted")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fallback placeholder remained: %v", err)
	}
}
