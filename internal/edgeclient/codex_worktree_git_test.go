//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCodexWorktreeGitMetadataRejectsExternalCommonDirectory(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	workspacePath := filepath.Join(devRoot, "linked")
	commonDir := filepath.Join(root, "outside", ".git")
	gitDir := filepath.Join(commonDir, "worktrees", "linked")
	for _, directory := range []string{workspacePath, gitDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspacePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveCodexWorktreeGitMetadata(Workspace{Path: workspacePath}, WorkspaceRoots{Dev: devRoot}); err == nil {
		t.Fatal("Git metadata outside the managed development root must fail")
	}
}

func TestResolveCodexWorktreeGitMetadataRejectsSymlinkPointer(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	workspacePath := filepath.Join(devRoot, "linked")
	if err := os.MkdirAll(workspacePath, 0o700); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(root, "git-pointer")
	if err := os.WriteFile(pointer, []byte("gitdir: /tmp/denied\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pointer, filepath.Join(workspacePath, ".git")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveCodexWorktreeGitMetadata(Workspace{Path: workspacePath}, WorkspaceRoots{Dev: devRoot}); err == nil {
		t.Fatal("a symlinked Git pointer must fail")
	}
}
