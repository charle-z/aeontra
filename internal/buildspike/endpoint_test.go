//go:build !windows

package buildspike

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateBuildKitSocketRejectsRootfulAndSymlinkedEndpoints(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "bk-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	uid := os.Geteuid()
	if uid == 0 {
		uid = 1001
	}
	runtimeRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(filepath.Join(runtimeRoot, "buildkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(runtimeRoot, "buildkit", "buildkitd.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, path := range []string{runtimeRoot, filepath.Join(runtimeRoot, "buildkit"), socket} {
		if err := os.Chown(path, uid, uid); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateBuildKitSocket(socket, runtimeRoot, uid); err != nil {
		t.Fatalf("valid socket rejected: %v", err)
	}
	for _, rootful := range []string{"/run/buildkit/buildkitd.sock", "/var/run/docker.sock", "/run/docker.sock"} {
		if err := ValidateBuildKitSocket(rootful, "/run", uid); err == nil {
			t.Fatalf("rootful endpoint accepted: %s", rootful)
		}
	}
	linkRoot := filepath.Join(root, "link")
	if err := os.Symlink(runtimeRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuildKitSocket(filepath.Join(linkRoot, "buildkit", "buildkitd.sock"), linkRoot, uid); err == nil {
		t.Fatal("symlinked endpoint accepted")
	}
}

func TestValidateBuildKitSocketRejectsWrongOwnerModeAndNonSocket(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o777); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "buildkitd.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBuildKitSocket(path, root, os.Geteuid()); err == nil {
		t.Fatal("non-socket accepted")
	}
}
