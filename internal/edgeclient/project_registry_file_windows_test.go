//go:build windows

package edgeclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsProjectRegistryCreatesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), projectRegistryFile)
	if err := prepareProjectRegistryFile(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("project registry was not retained: info=%v err=%v", info, err)
	}
	if err := prepareProjectRegistryFile(path); err != nil {
		t.Fatalf("reopen project registry file: %v", err)
	}
}
