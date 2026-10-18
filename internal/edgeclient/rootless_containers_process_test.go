//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExecContainerCommandRunnerCancelsWholeProcessGroup(t *testing.T) {
	root := t.TempDir()
	heartbeat := filepath.Join(root, "heartbeat")
	pidPath := filepath.Join(root, "child.pid")
	script := `heartbeat=$1; pidfile=$2; (trap '' TERM; while :; do printf x >>"$heartbeat"; sleep 0.05; done) & child=$!; printf '%s\n' "$child" >"$pidfile"; wait "$child"`

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := execContainerCommandRunner{}.Run(ctx, "/bin/sh", []string{"-c", script, "p12", heartbeat, pidPath}, []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"})
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		pidBody, pidErr := os.ReadFile(pidPath)
		info, heartbeatErr := os.Stat(heartbeat)
		if pidErr == nil && heartbeatErr == nil && info.Size() >= 3 {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBody)))
			if parseErr == nil && parsed > 1 {
				childPID = parsed
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if childPID == 0 {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		t.Fatal("child process did not become ready")
	}
	t.Cleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled rootless command unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rootless command did not return after cancellation")
	}

	time.Sleep(150 * time.Millisecond)
	before, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	after, err := os.Stat(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("child process survived cancellation: heartbeat grew from %d to %d", before.Size(), after.Size())
	}
}
