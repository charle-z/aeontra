//go:build windows

package edgeclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsAttestationUsesAuthorizedRoot(t *testing.T) {
	roots := WorkspaceRoots{
		Dev:        `C:\Users\tester\workspaces`,
		HTBLinux:   `C:\Users\tester\htb-machines`,
		WindowsDev: `D:\Aeontra\Workspaces`,
	}
	root, candidate, err := windowsAttestationRoot(`d:\aeontra\workspaces\codex`, &roots)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(root, roots.WindowsDev) || !strings.EqualFold(candidate, `D:\Aeontra\Workspaces\codex`) {
		t.Fatalf("root=%q candidate=%q", root, candidate)
	}
	if _, _, err := windowsAttestationRoot(roots.WindowsDev, &roots); err == nil {
		t.Fatal("Windows workcell root itself was accepted as a project")
	}
	root, candidate, err = windowsAttestationRoot(`D:\Aeontra\Workspaces\codex`, nil)
	if err != nil || !strings.EqualFold(root, candidate) {
		t.Fatalf("unrooted attestation root=%q candidate=%q err=%v", root, candidate, err)
	}
}

func TestWindowsGitCommonDirectoryRejectsNonRegularMarker(t *testing.T) {
	gitDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(gitDir, "commondir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveGitCommonDirectory(gitDir); err == nil {
		t.Fatal("directory commondir marker was accepted")
	}
}
