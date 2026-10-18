package observability

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledDistinguishesOffFromActiveSink(t *testing.T) {
	off, err := Open(Config{Mode: ModeOff, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if off.Enabled() {
		t.Fatal("off logger reports enabled")
	}
	active, err := Open(Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !active.Enabled() {
		t.Fatal("stderr logger reports disabled")
	}
}

func TestFileModeRejectsSymlinkFileWithoutLeakingPath(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "private-target.jsonl")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "customer-secret-link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := Open(Config{Mode: ModeFile, Path: link, MaxBytes: MinMaxBytes}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("symlink file should be rejected")
	}
	if strings.Contains(err.Error(), "customer-secret") || strings.Contains(err.Error(), target) {
		t.Fatalf("error leaked private path: %v", err)
	}
}

func TestFileModeRejectsSymlinkDirectory(t *testing.T) {
	realDirectory := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "linked-private-directory")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := Open(Config{Mode: ModeFile, Path: filepath.Join(link, "events.jsonl"), MaxBytes: MinMaxBytes}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("symlink directory should be rejected")
	}
}

func TestFileModeRejectsBroadExistingDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "broad")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Config{Mode: ModeFile, Path: filepath.Join(directory, "events.jsonl"), MaxBytes: MinMaxBytes}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("broad directory should be rejected")
	}
}
