package observability

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileModeRejectsSymlinkAncestor(t *testing.T) {
	realParent := t.TempDir()
	outer := t.TempDir()
	link := filepath.Join(outer, "linked-parent")
	if err := os.Symlink(realParent, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(link, "nested", "events.jsonl")
	_, err := Open(Config{Mode: ModeFile, Path: path, MaxBytes: MinMaxBytes}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("symlink ancestor should be rejected")
	}
}
