//go:build !windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectProcessAttestationAllowsNormalWorkspaceMutation(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	attestation, err := captureProjectProcessWorkspaceAttestation(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "source.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessWorkspaceAttestation(workspace, workspace, attestation); err != nil {
		t.Fatalf("normal source mutation was rejected: %v", err)
	}
}

func TestProjectProcessAttestationRejectsWorkspaceReplacement(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	attestation, err := captureProjectProcessWorkspaceAttestation(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(workspace, workspace+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessWorkspaceAttestation(workspace, workspace, attestation); !errors.Is(err, ErrProjectProcessIdentityChanged) {
		t.Fatalf("replacement err=%v, want identity change", err)
	}
}

func TestProjectProcessAttestationRejectsGitMetadataReplacement(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	attestation, err := captureProjectProcessWorkspaceAttestation(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(workspace, ".git"), filepath.Join(workspace, ".git.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessWorkspaceAttestation(workspace, workspace, attestation); !errors.Is(err, ErrProjectProcessIdentityChanged) {
		t.Fatalf("Git replacement err=%v, want identity change", err)
	}
}

func TestProjectProcessAttestationRejectsCWDSymlink(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	attestation, err := captureProjectProcessWorkspaceAttestation(workspace, filepath.Join(workspace, "src"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(workspace, "src"), filepath.Join(workspace, "src.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workspace, "src.old"), filepath.Join(workspace, "src")); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessWorkspaceAttestation(workspace, filepath.Join(workspace, "src"), attestation); !errors.Is(err, ErrProjectProcessIdentityChanged) {
		t.Fatalf("CWD symlink err=%v, want identity change", err)
	}
}

func projectProcessRuntimeAttestationFixture(t *testing.T) (string, ProjectRuntimeRoots) {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := ProjectRuntimeRoots{
		Runtime:   filepath.Join(stateRoot, "project-runtime", "ws_0123456789abcdef0123456789abcdef"),
		Cache:     filepath.Join(stateRoot, "project-cache", "ws_0123456789abcdef0123456789abcdef"),
		Artifacts: filepath.Join(stateRoot, "project-artifacts", "ws_0123456789abcdef0123456789abcdef"),
	}
	for _, root := range []string{roots.Runtime, roots.Cache, roots.Artifacts} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return stateRoot, roots
}

func TestProjectProcessRuntimeAttestationAllowsCacheMutation(t *testing.T) {
	stateRoot, roots := projectProcessRuntimeAttestationFixture(t)
	attestation, err := captureProjectProcessRuntimeAttestation(stateRoot, roots)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roots.Cache, "toolchain-index"), []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessRuntimeAttestation(stateRoot, roots, attestation); err != nil {
		t.Fatalf("normal runtime cache mutation was rejected: %v", err)
	}
}

func TestProjectProcessRuntimeAttestationRejectsRootReplacement(t *testing.T) {
	stateRoot, roots := projectProcessRuntimeAttestationFixture(t)
	attestation, err := captureProjectProcessRuntimeAttestation(stateRoot, roots)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(roots.Runtime, roots.Runtime+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(roots.Runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := revalidateProjectProcessRuntimeAttestation(stateRoot, roots, attestation); !errors.Is(err, ErrProjectProcessIdentityChanged) {
		t.Fatalf("runtime replacement err=%v, want identity change", err)
	}
}
