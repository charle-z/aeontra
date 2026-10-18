//go:build !windows

package edgelifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRenameNoReplaceDoesNotUseDirectoryFallbackForRegularFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "journal.tmp")
	destination := filepath.Join(root, "journal.json")
	if err := os.WriteFile(source, []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := renameNoReplaceWith(source, destination, func(_, _ string) error { return unix.EOPNOTSUPP })
	if !errors.Is(err, unix.EOPNOTSUPP) {
		t.Fatalf("error=%v", err)
	}
	content, statErr := os.ReadFile(source)
	if statErr != nil || string(content) != "journal" {
		t.Fatalf("source changed: content=%q err=%v", content, statErr)
	}
	if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination appeared: %v", statErr)
	}
}
