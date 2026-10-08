//go:build !windows

package sandboxexecutor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRejectsNonPrivateStateRoot(t *testing.T) {
	workspace := t.TempDir()
	state := t.TempDir()
	if err := os.Chmod(state, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{
		Token: strings.Repeat("t", 32), WorkspaceID: "primary", WorkspaceRoot: workspace,
		StateRoot: state, Image: "image@" + executorTestDigest, ImageDigest: executorTestDigest,
		MaxTimeoutMS: 1000, MaxCPUMillis: 100, MaxMemoryMiB: 128, MaxProcessLimit: 8,
		MaxOutputBytes: 1024, MaxConcurrent: 1, Engine: &fakeEngine{},
	})
	if err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("non-private state root accepted: %v", err)
	}
}

func TestNewRejectsSymlinkReceiptRoot(t *testing.T) {
	workspace := t.TempDir()
	state := t.TempDir()
	receipts := filepath.Join(state, "receipts")
	if err := os.Remove(receipts); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, receipts); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{
		Token: strings.Repeat("t", 32), WorkspaceID: "primary", WorkspaceRoot: workspace,
		StateRoot: state, Image: "image@" + executorTestDigest, ImageDigest: executorTestDigest,
		MaxTimeoutMS: 1000, MaxCPUMillis: 100, MaxMemoryMiB: 128, MaxProcessLimit: 8,
		MaxOutputBytes: 1024, MaxConcurrent: 1, Engine: &fakeEngine{},
	})
	if err == nil {
		t.Fatal("symlink receipt root accepted")
	}
}
