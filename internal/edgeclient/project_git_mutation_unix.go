//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runContainedDevGitFastForward permits Git to apply tracked-tree changes while
// keeping repository hooks and filters inside a networkless mount namespace. The
// checkout is the only host path writable by the mutation.
func runContainedDevGitFastForward(ctx context.Context, dir, gitPath, toolPath string, gitArgs []string) (string, error) {
	if !devGitFastForwardArguments(gitArgs) {
		return "", errors.New("contained Git mutation is invalid")
	}
	workspace, err := ValidateRegisteredWorkspace(dir)
	if err != nil || filepath.Clean(workspace) != filepath.Clean(dir) {
		return "", errors.New("contained Git workspace is unsafe")
	}
	bubblewrap, err := resolveDirectWorkcellBubblewrap()
	if err != nil {
		return "", errors.New("contained Git mutation runner is unavailable")
	}
	args, err := containedDevGitFastForwardArgs(workspace, gitPath, gitArgs)
	if err != nil {
		return "", err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, bubblewrap, args...)
	command.Dir = workspace
	command.Env = []string{"PATH=" + toolPath, "HOME=" + workspace, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	output := &boundedHTBLabCapture{limit: 1 << 20}
	command.Stdout = output
	command.Stderr = output
	runErr := command.Run()
	return output.buffer.String(), runErr
}

func containedDevGitFastForwardArgs(workspace, gitPath string, gitArgs []string) ([]string, error) {
	if !filepath.IsAbs(workspace) || !filepath.IsAbs(gitPath) || !devGitFastForwardArguments(gitArgs) {
		return nil, errors.New("contained Git mutation is invalid")
	}
	args := []string{"--die-with-parent", "--new-session", "--unshare-all", "--clearenv"}
	for _, systemPath := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64"} {
		if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", systemPath, systemPath)
		}
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--bind", workspace, "/workspace", "--chdir", "/workspace",
		"--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin",
		"--setenv", "HOME", "/tmp",
		"--setenv", "LANG", "C.UTF-8",
		"--setenv", "LC_ALL", "C.UTF-8",
		"--setenv", "GIT_TERMINAL_PROMPT", "0",
		"--setenv", "GIT_CONFIG_NOSYSTEM", "1",
		"--setenv", "GIT_CONFIG_GLOBAL", "/dev/null",
		"--setenv", "GIT_CONFIG_SYSTEM", "/dev/null",
		"--setenv", "GIT_OPTIONAL_LOCKS", "0",
		"--", gitPath,
	)
	args = append(args, devGitProtectedArguments(gitArgs, "")...)
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("contained Git mutation is invalid")
		}
	}
	return args, nil
}
