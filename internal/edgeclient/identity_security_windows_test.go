//go:build windows

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
	if info.IsDir() {
		err = validatePrivateRoot(path)
	} else {
		err = requirePrivateRegularFile(path)
	}
	if err != nil {
		t.Fatalf("private Windows path %s is unsafe: %v", path, err)
	}
}
