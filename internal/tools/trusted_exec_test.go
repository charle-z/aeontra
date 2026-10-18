package tools

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveTrustedExecutableRejectsWorkspaceControlledPath(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "git")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := resolveTrustedExecutableWith("git", []string{root}, func(string) (string, error) {
		return executable, nil
	})
	if !errors.Is(err, ErrWorkspaceExecutable) {
		t.Fatalf("error = %v, want ErrWorkspaceExecutable", err)
	}
}

func TestResolveTrustedExecutableRejectsRelativeLookupResult(t *testing.T) {
	_, err := resolveTrustedExecutableWith("git", nil, func(string) (string, error) {
		return "git", nil
	})
	if !errors.Is(err, ErrUntrustedExecutablePath) {
		t.Fatalf("error = %v, want ErrUntrustedExecutablePath", err)
	}
}

func TestResolveTrustedExecutableAllowsAbsolutePathOutsideWorkspace(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	trustedDir := filepath.Join(base, "trusted-bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trustedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(trustedDir, "git")
	if err := os.WriteFile(executable, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveTrustedExecutableWith("git", []string{root}, func(string) (string, error) {
		return executable, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != executable {
		t.Fatalf("resolved = %q, want %q", got, executable)
	}
}

func TestResolveTrustedExecutableRejectsSymlinkIntoWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	insideExecutable := filepath.Join(root, "git")
	if err := os.WriteFile(insideExecutable, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(outside, "git")
	if err := os.Symlink(insideExecutable, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolveTrustedExecutableWith("git", []string{root}, func(string) (string, error) {
		return link, nil
	})
	if !errors.Is(err, ErrWorkspaceExecutable) {
		t.Fatalf("error = %v, want ErrWorkspaceExecutable", err)
	}
}
