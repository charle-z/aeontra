//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsProcessTreeLimits are administrator-owned limits applied to one Job
// Object. A zero value is not an unlimited profile: all fields are required so
// callers cannot accidentally create an unbounded workcell process.
type WindowsProcessTreeLimits struct {
	MaxProcesses uint32
	MemoryBytes  uint64
	CPUTime      time.Duration
	WallTime     time.Duration
}

const (
	maxWindowsProcessTreeProcesses uint32 = 4096
	maxWindowsProcessTreeMemory    uint64 = 1 << 40
	maxWindowsProcessTreeDuration         = 24 * time.Hour
	windowsJobTimeQuantum                 = 100 * time.Nanosecond
	windowsProcessStillActive      uint32 = 259
)

var (
	ErrWindowsProcessTreeClosed            = errors.New("windows process tree is closed")
	ErrWindowsProcessTreeIdentityChanged   = errors.New("windows process identity changed")
	ErrWindowsProcessTreeLimitsInvalid     = errors.New("windows process tree limits are invalid")
	ErrWindowsProcessTreeBreakawayRequest  = errors.New("windows process tree breakaway is not permitted")
	ErrWindowsProcessTreeThreadUnavailable = errors.New("windows process tree primary thread is unavailable")
)

// WindowsProcessIdentity binds a PID to the kernel creation-time identity of
// that process. A PID by itself is not sufficient because Windows can reuse it
// after the original process exits.
type WindowsProcessIdentity struct {
	ProcessID    uint32
	CreationTime uint64
}

// WindowsProcessTree owns the Job Object and the command assigned to it. The
// command must be waited through this type after Start; doing so closes the Job
// Object after the root exits and prevents orphaned descendants.
type WindowsProcessTree struct {
	startMu  sync.Mutex
	mu       sync.Mutex
	job      windows.Handle
	command  *exec.Cmd
	identity WindowsProcessIdentity
	limits   WindowsProcessTreeLimits
	closed   bool
	done     chan struct{}
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

// NewWindowsProcessTree creates a private Job Object configured to kill its
// entire process tree when the last owner handle closes. Breakaway is not
// enabled; the configured process, memory and CPU limits are enforced by the
// kernel. Wall time is enforced by the watchdog started by Start.
func NewWindowsProcessTree(limits WindowsProcessTreeLimits) (*WindowsProcessTree, error) {
	if err := validateWindowsProcessTreeLimits(limits); err != nil {
		return nil, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS |
		windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_JOB_MEMORY |
		windows.JOB_OBJECT_LIMIT_JOB_TIME
	info.BasicLimitInformation.ActiveProcessLimit = limits.MaxProcesses
	info.BasicLimitInformation.PerJobUserTimeLimit = int64(limits.CPUTime / windowsJobTimeQuantum)
	info.ProcessMemoryLimit = uintptr(limits.MemoryBytes)
	info.JobMemoryLimit = uintptr(limits.MemoryBytes)
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &WindowsProcessTree{
		job:      job,
		limits:   limits,
		done:     make(chan struct{}),
		waitDone: make(chan struct{}),
	}, nil
}

func validateWindowsProcessTreeLimits(limits WindowsProcessTreeLimits) error {
	if limits.MaxProcesses < 1 || limits.MaxProcesses > maxWindowsProcessTreeProcesses ||
		limits.MemoryBytes < 1<<20 || limits.MemoryBytes > maxWindowsProcessTreeMemory ||
		limits.CPUTime <= 0 || limits.CPUTime > maxWindowsProcessTreeDuration ||
		limits.WallTime <= 0 || limits.WallTime > maxWindowsProcessTreeDuration ||
		limits.CPUTime < windowsJobTimeQuantum {
		return ErrWindowsProcessTreeLimitsInvalid
	}
	if uint64(uintptr(limits.MemoryBytes)) != limits.MemoryBytes {
		return ErrWindowsProcessTreeLimitsInvalid
	}
	return nil
}

// Limits returns the immutable limits configured for this tree.
func (tree *WindowsProcessTree) Limits() WindowsProcessTreeLimits {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	return tree.limits
}

// Start launches cmd suspended, assigns it to this Job Object, captures its
// creation-time identity, and resumes its primary thread. Assigning before
// resume closes the race in which a child could otherwise escape before job
// ownership is established. The context cancels the complete tree.
func (tree *WindowsProcessTree) Start(ctx context.Context, cmd *exec.Cmd) error {
	tree.startMu.Lock()
	defer tree.startMu.Unlock()
	if cmd == nil || cmd.Path == "" {
		return errors.New("windows process tree command is invalid")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if attr := cmd.SysProcAttr; attr != nil && attr.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB != 0 {
		return ErrWindowsProcessTreeBreakawayRequest
	}

	tree.mu.Lock()
	if tree.closed {
		tree.mu.Unlock()
		return ErrWindowsProcessTreeClosed
	}
	if tree.command != nil {
		tree.mu.Unlock()
		return errors.New("windows process tree already started")
	}
	// os/exec exposes the suspended creation flag through the Windows process
	// attributes. Preserve caller flags, but never add a breakaway flag.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	tree.mu.Unlock()

	if err := cmd.Start(); err != nil {
		return err
	}
	failStart := func(startErr error) error {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = tree.Close()
		return startErr
	}
	identity, err := windowsProcessIdentity(cmd.Process)
	if err != nil {
		return failStart(err)
	}
	tree.mu.Lock()
	tree.identity = identity
	tree.command = cmd
	job := tree.job
	tree.mu.Unlock()
	var assignErr error
	withHandleErr := cmd.Process.WithHandle(func(raw uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(raw))
	})
	if withHandleErr != nil {
		return failStart(withHandleErr)
	}
	if assignErr != nil {
		return failStart(assignErr)
	}
	if err := resumeWindowsProcessPrimaryThread(uint32(cmd.Process.Pid)); err != nil {
		return failStart(err)
	}
	go tree.watch(ctx)
	return nil
}

func windowsProcessIdentity(process *os.Process) (WindowsProcessIdentity, error) {
	if process == nil || process.Pid < 1 {
		return WindowsProcessIdentity{}, errors.New("windows process identity is invalid")
	}
	var identity WindowsProcessIdentity
	var identityErr error
	handleErr := process.WithHandle(func(raw uintptr) {
		identity, identityErr = windowsProcessIdentityFromHandle(windows.Handle(raw), uint32(process.Pid))
	})
	if handleErr != nil {
		return WindowsProcessIdentity{}, handleErr
	}
	if identityErr != nil {
		return WindowsProcessIdentity{}, identityErr
	}
	return identity, nil
}

func windowsProcessIdentityFromHandle(handle windows.Handle, processID uint32) (WindowsProcessIdentity, error) {
	if handle == 0 || processID == 0 {
		return WindowsProcessIdentity{}, errors.New("windows process identity is invalid")
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return WindowsProcessIdentity{}, err
	}
	creationTime := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	if creationTime == 0 {
		return WindowsProcessIdentity{}, errors.New("windows process creation time is unavailable")
	}
	return WindowsProcessIdentity{ProcessID: processID, CreationTime: creationTime}, nil
}

// Identity returns the opaque identity captured at process start.
func (tree *WindowsProcessTree) Identity() WindowsProcessIdentity {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	return tree.identity
}

// VerifyIdentity re-reads the process creation time for identity and fails
// closed if the PID has been reused or no longer refers to the owned process.
func (tree *WindowsProcessTree) VerifyIdentity(identity WindowsProcessIdentity) error {
	if identity.ProcessID == 0 || identity.CreationTime == 0 {
		return ErrWindowsProcessTreeIdentityChanged
	}
	tree.mu.Lock()
	owned := tree.identity
	tree.mu.Unlock()
	if owned != identity {
		return ErrWindowsProcessTreeIdentityChanged
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, identity.ProcessID)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return ErrWindowsProcessTreeIdentityChanged
		}
		return err
	}
	defer windows.CloseHandle(process)
	current, err := windowsProcessIdentityFromHandle(process, identity.ProcessID)
	if err != nil {
		return err
	}
	if current != identity {
		return ErrWindowsProcessTreeIdentityChanged
	}
	return nil
}

// Alive reports whether identity still names the same live process. It never
// treats a matching PID alone as ownership proof.
func (tree *WindowsProcessTree) Alive(identity WindowsProcessIdentity) (bool, error) {
	if err := tree.VerifyIdentity(identity); err != nil {
		if errors.Is(err, ErrWindowsProcessTreeIdentityChanged) {
			return false, nil
		}
		return false, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, identity.ProcessID)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(process)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return false, err
	}
	return exitCode == windowsProcessStillActive, nil
}

// Terminate kills every process currently in the Job Object. It is safe to
// call repeatedly, and is the explicit stop primitive for foreground and
// durable process lifecycles.
func (tree *WindowsProcessTree) Terminate() error {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.closed || tree.job == 0 {
		return ErrWindowsProcessTreeClosed
	}
	return windows.TerminateJobObject(tree.job, 1)
}

// Wait waits for the root command, then closes the Job Object so descendants
// cannot survive a root exit. Wait is idempotent and safe for one or more
// lifecycle observers.
func (tree *WindowsProcessTree) Wait() error {
	tree.waitOnce.Do(func() {
		tree.mu.Lock()
		cmd := tree.command
		tree.mu.Unlock()
		if cmd == nil {
			tree.waitErr = errors.New("windows process tree has not started")
			close(tree.waitDone)
			return
		}
		tree.waitErr = cmd.Wait()
		_ = tree.Close()
		close(tree.waitDone)
	})
	<-tree.waitDone
	return tree.waitErr
}

// Close releases the Job Object. The configured kill-on-close limit makes
// release a cleanup boundary for the complete process tree.
func (tree *WindowsProcessTree) Close() error {
	tree.mu.Lock()
	if tree.closed {
		tree.mu.Unlock()
		return nil
	}
	tree.closed = true
	job := tree.job
	tree.job = 0
	close(tree.done)
	tree.mu.Unlock()
	if job == 0 {
		return nil
	}
	return windows.CloseHandle(job)
}

func (tree *WindowsProcessTree) watch(ctx context.Context) {
	timer := time.NewTimer(tree.limits.WallTime)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = tree.Terminate()
	case <-timer.C:
		_ = tree.Terminate()
	case <-tree.done:
	}
}

func resumeWindowsProcessPrimaryThread(processID uint32) error {
	if processID == 0 {
		return ErrWindowsProcessTreeThreadUnavailable
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == processID {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			previous, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			if resumeErr != nil {
				return resumeErr
			}
			if previous == ^uint32(0) {
				return ErrWindowsProcessTreeThreadUnavailable
			}
			return nil
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return ErrWindowsProcessTreeThreadUnavailable
			}
			return err
		}
	}
}
