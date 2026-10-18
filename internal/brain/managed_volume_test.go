package brain

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEmptyManagedVolumeRootCanBeHardened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes are required")
	}
	root := filepath.Join(t.TempDir(), "brain")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ensurePrivateRootDirectory(root); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("managed volume root mode=%#o want=0700", got)
	}
}

func TestNonEmptyUnsafeRootCannotBeHardened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory modes are required")
	}
	root := filepath.Join(t.TempDir(), "brain")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unexpected"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ensurePrivateRootDirectory(root)
	if err == nil || !strings.Contains(err.Error(), "permissions must be 0700") {
		t.Fatalf("non-empty unsafe root must fail closed: %v", err)
	}
	info, statErr := os.Lstat(root)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("rejected non-empty root was modified: mode=%#o", got)
	}
}

func TestManagedVolumeRootSelectionIsExact(t *testing.T) {
	if !isManagedVolumeRoot("/brain") {
		t.Fatal("exact production managed volume root was not selected")
	}
	for _, other := range []string{"/brain-other", "/tmp/brain", "brain"} {
		if isManagedVolumeRoot(other) {
			t.Fatalf("non-production root %q selected managed-volume hardening", other)
		}
	}
}
