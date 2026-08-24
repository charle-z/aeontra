//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func testWindowsProcessTreeLimits() WindowsProcessTreeLimits {
	return WindowsProcessTreeLimits{
		MaxProcesses: 8,
		MemoryBytes:  64 << 20,
		CPUTime:      time.Minute,
		WallTime:     2 * time.Minute,
	}
}

func TestWindowsProcessTreeRejectsUnboundedOrUnsafeLimits(t *testing.T) {
	for _, limits := range []WindowsProcessTreeLimits{
		{},
		{MaxProcesses: 1, MemoryBytes: 64 << 20, CPUTime: time.Minute},
		{MaxProcesses: 0, MemoryBytes: 64 << 20, CPUTime: time.Minute, WallTime: time.Minute},
		{MaxProcesses: 1, MemoryBytes: 64 << 20, CPUTime: 0, WallTime: time.Minute},
		{MaxProcesses: 1, MemoryBytes: 64 << 20, CPUTime: 25 * time.Hour, WallTime: time.Minute},
	} {
		if _, err := NewWindowsProcessTree(limits); !errors.Is(err, ErrWindowsProcessTreeLimitsInvalid) {
			t.Fatalf("limits=%+v err=%v, want invalid limits", limits, err)
		}
	}
}

func TestWindowsProcessTreeConfiguresKillOwnershipAndResourceLimits(t *testing.T) {
	limits := testWindowsProcessTreeLimits()
	tree, err := NewWindowsProcessTree(limits)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(tree.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		t.Fatal(err)
	}
	flags := info.BasicLimitInformation.LimitFlags
	if flags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 || flags&windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS == 0 ||
		flags&windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY == 0 || flags&windows.JOB_OBJECT_LIMIT_JOB_MEMORY == 0 || flags&windows.JOB_OBJECT_LIMIT_JOB_TIME == 0 {
		t.Fatalf("job flags=%#x, missing ownership/resource limits", flags)
	}
	if flags&(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK) != 0 {
		t.Fatalf("job flags=%#x, breakaway is enabled", flags)
	}
	if info.BasicLimitInformation.ActiveProcessLimit != limits.MaxProcesses || info.ProcessMemoryLimit != uintptr(limits.MemoryBytes) || info.JobMemoryLimit != uintptr(limits.MemoryBytes) {
		t.Fatalf("job limits=%+v, want processes=%d memory=%d", info, limits.MaxProcesses, limits.MemoryBytes)
	}
}

func TestWindowsProcessTreeRejectsBreakawayBeforeStarting(t *testing.T) {
	tree, err := NewWindowsProcessTree(testWindowsProcessTreeLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB}
	if err := tree.Start(context.Background(), cmd); !errors.Is(err, ErrWindowsProcessTreeBreakawayRequest) {
		t.Fatalf("start err=%v, want breakaway rejection", err)
	}
}

func TestWindowsProcessTreeCapturesCreationTimeAndWaitsIdempotently(t *testing.T) {
	tree, err := NewWindowsProcessTree(testWindowsProcessTreeLimits())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	if err := tree.Start(context.Background(), cmd); err != nil {
		t.Fatalf("start: %v", err)
	}
	identity := tree.Identity()
	if identity.ProcessID == 0 || identity.CreationTime == 0 {
		t.Fatalf("identity=%+v, want PID and creation time", identity)
	}
	if err := tree.VerifyIdentity(identity); err != nil {
		t.Fatalf("verify identity: %v", err)
	}
	wrong := identity
	wrong.CreationTime++
	if err := tree.VerifyIdentity(wrong); !errors.Is(err, ErrWindowsProcessTreeIdentityChanged) {
		t.Fatalf("wrong identity err=%v", err)
	}
	if err := tree.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if err := tree.Wait(); err != nil {
		t.Fatalf("second wait: %v", err)
	}
}

func TestWindowsProcessTreeCloseTerminatesRoot(t *testing.T) {
	tree, err := NewWindowsProcessTree(testWindowsProcessTreeLimits())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/c", "ping 127.0.0.1 -n 30 > nul")
	if err := tree.Start(context.Background(), cmd); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("root process survived Job Object close")
	}
}
