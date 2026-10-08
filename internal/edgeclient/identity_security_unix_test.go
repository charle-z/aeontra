//go:build !windows

package edgeclient

import (
	"os"
	"testing"
)

func assertPrivateIdentityPath(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("permissions %s=%o", path, info.Mode().Perm())
	}
}
