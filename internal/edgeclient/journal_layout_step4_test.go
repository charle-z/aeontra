package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStep4OpenJournalRejectsWeakModeAndOversizedExistingFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "journal.db")
	if err := os.WriteFile(path, []byte("unsafe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(root); err == nil {
		t.Fatal("journal with group-readable mode was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, (32<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(root); err == nil {
		t.Fatal("oversized existing journal was accepted")
	}
}
