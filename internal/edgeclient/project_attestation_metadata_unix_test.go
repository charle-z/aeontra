//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitCommonDirectoryRejectsSymlinkMarker(t *testing.T) {
	gitDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("../common"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(gitDir, "commondir")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveGitCommonDirectory(gitDir); err == nil {
		t.Fatal("symlink commondir marker was accepted")
	}
}
