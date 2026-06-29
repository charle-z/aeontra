package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustJail(t *testing.T, roots ...string) *Jail {
	t.Helper()
	j, err := NewJail(roots)
	if err != nil {
		t.Fatalf("NewJail(%v): %v", roots, err)
	}
	return j
}

func TestJail_AllowsPathsInsideRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "file.go")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := mustJail(t, root)

	got, err := j.Resolve(sub)
	if err != nil {
		t.Fatalf("expected inside-root path to resolve, got %v", err)
	}
	if !j.Contains(got) {
		t.Fatalf("resolved path %q should be contained", got)
	}
	// Relative paths resolve against the root too.
	if _, err := j.Resolve(filepath.Join("pkg", "file.go")); err != nil {
		t.Fatalf("relative inside-root path should resolve: %v", err)
	}
	// A not-yet-existing file inside the root is allowed (patch will create it).
	if _, err := j.Resolve(filepath.Join(root, "new", "created.txt")); err != nil {
		t.Fatalf("non-existent inside-root path should resolve: %v", err)
	}
}

func TestJail_BlocksTraversal(t *testing.T) {
	root := t.TempDir()
	j := mustJail(t, root)
	cases := []string{
		filepath.Join(root, "..", "escape.txt"),
		filepath.Join(root, "a", "..", "..", "escape.txt"),
		"../../../../etc/passwd",
	}
	for _, c := range cases {
		if _, err := j.Resolve(c); err == nil {
			t.Errorf("traversal %q should be blocked", c)
		}
	}
}

func TestJail_BlocksAbsoluteOutside(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir() // a sibling temp dir, definitely outside root
	j := mustJail(t, root)
	if _, err := j.Resolve(filepath.Join(other, "secret.txt")); err == nil {
		t.Errorf("absolute path outside root should be blocked")
	}
}

func TestJail_BlocksSiblingPrefix(t *testing.T) {
	// "/x/repo-evil" must not be considered inside "/x/repo".
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	evil := filepath.Join(base, "repo-evil")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	j := mustJail(t, root)
	if _, err := j.Resolve(filepath.Join(evil, "f.txt")); err == nil {
		t.Errorf("sibling-prefix path %q should be blocked", evil)
	}
}

func TestJail_BlocksSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the root pointing outside it.
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation not permitted on this host: %v", err)
		}
		t.Fatal(err)
	}
	j := mustJail(t, root)
	if _, err := j.Resolve(filepath.Join(link, "secret.txt")); err == nil {
		t.Errorf("symlink escape via %q should be blocked", link)
	}
}

func TestJail_BlocksUNCAndDevicePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC/device paths are Windows-specific")
	}
	root := t.TempDir()
	j := mustJail(t, root)
	for _, p := range []string{`\\server\share\f.txt`, `\\?\C:\Windows\system32`, `\\.\PhysicalDrive0`} {
		if _, err := j.Resolve(p); err == nil {
			t.Errorf("UNC/device path %q should be blocked", p)
		}
	}
}

func TestNewJail_RejectsRelativeRoot(t *testing.T) {
	if _, err := NewJail([]string{"relative/root"}); err == nil {
		t.Errorf("relative root should be rejected")
	}
	if _, err := NewJail(nil); err == nil {
		t.Errorf("empty roots should be rejected")
	}
}
