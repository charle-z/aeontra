//go:build !windows

package edgeclient

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testEdgeRelease = "p15.0.9"
const testEdgeCommit = "0123456789abcdef0123456789abcdef01234567"

func TestEdgeInstanceLockRejectsSecondInstanceForSameStateRoot(t *testing.T) {
	stateRoot := t.TempDir()
	first, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	report, err := InspectEdgeInstanceLock(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "held" || !report.LockHeld || !report.ProcessActive || report.Metadata.PID != os.Getpid() {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit); !errors.Is(err, ErrEdgeInstanceLocked) {
		t.Fatalf("second instance error = %v", err)
	}
}

func TestEdgeInstanceLockIsolatedByStateRoot(t *testing.T) {
	first, err := AcquireEdgeInstanceLock(filepath.Join(t.TempDir(), "first"), testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := AcquireEdgeInstanceLock(filepath.Join(t.TempDir(), "second"), testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
}

func TestEdgeInstanceLockCleanCloseAllowsReacquisition(t *testing.T) {
	stateRoot := t.TempDir()
	first, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := InspectEdgeInstanceLock(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "unlocked_live_owner" || report.StaleRecoverable || report.LockHeld {
		t.Fatalf("unexpected released report: %+v", report)
	}
	second, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
}

func TestEdgeInstanceLockSurvivesUnrelatedUnlockAttempt(t *testing.T) {
	stateRoot := t.TempDir()
	owner, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	other, err := os.OpenFile(EdgeInstanceLockPath(stateRoot), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(other.Fd()), unix.LOCK_UN); err != nil {
		other.Close()
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit); !errors.Is(err, ErrEdgeInstanceLocked) {
		t.Fatalf("unrelated descriptor released owner lock: %v", err)
	}
}

func TestEdgeInstanceLockRecoversObsoleteUnlockedMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(EdgeInstanceLockPath(stateRoot), []byte("legacy-lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectEdgeInstanceLock(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.State != "stale_recoverable" || !report.StaleRecoverable || report.LockHeld {
		t.Fatalf("obsolete metadata was not recoverable: %+v", report)
	}
	lock, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatalf("obsolete metadata blocked acquisition: %v", err)
	}
	defer lock.Close()
}

func TestEdgeInstanceLockRejectsMismatchedSystemdExecPID(t *testing.T) {
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	t.Setenv("SYSTEMD_EXEC_PID", strconv.Itoa(os.Getpid()+1))
	stateRoot := t.TempDir()
	lock, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	report, err := InspectEdgeInstanceLock(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if report.Metadata.InvocationID != "" {
		t.Fatalf("mismatched systemd PID was accepted: %+v", report.Metadata)
	}
}

func TestEdgeInstanceLockRecoversAfterAbruptProcessDeath(t *testing.T) {
	stateRoot := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=TestEdgeInstanceLockHelperProcess")
	command.Env = append(os.Environ(), "MCP_DEVBOX_EDGE_LOCK_HELPER=1", "MCP_DEVBOX_EDGE_LOCK_STATE="+stateRoot)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper did not acquire lock: %q", scanner.Text())
	}
	if _, err := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit); !errors.Is(err, ErrEdgeInstanceLocked) {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("live helper did not block second instance: %v", err)
	}
	if err := command.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for {
		lock, acquireErr := AcquireEdgeInstanceLock(stateRoot, testEdgeRelease, testEdgeCommit)
		if acquireErr == nil {
			_ = lock.Close()
			break
		}
		if !errors.Is(acquireErr, ErrEdgeInstanceLocked) || time.Now().After(deadline) {
			t.Fatalf("lock did not recover after process death: %v", acquireErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEdgeInstanceLockHelperProcess(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_EDGE_LOCK_HELPER") != "1" {
		return
	}
	lock, err := AcquireEdgeInstanceLock(os.Getenv("MCP_DEVBOX_EDGE_LOCK_STATE"), testEdgeRelease, testEdgeCommit)
	if err != nil {
		fmt.Fprintln(os.Stdout, "failed")
		os.Exit(2)
	}
	defer lock.Close()
	fmt.Fprintln(os.Stdout, "locked")
	for {
		time.Sleep(time.Hour)
	}
}
