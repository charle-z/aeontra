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

const (
	projectGitCommandTimeout = 10 * time.Second
	projectGitStatusTimeout  = 2 * time.Minute
)

type localProjectCheckoutInspector struct{}

func newProjectCheckoutInspector() ProjectCheckoutInspector {
	return localProjectCheckoutInspector{}
}

func (localProjectCheckoutInspector) Inspect(ctx context.Context, path, owner, repository string) (ProjectCheckoutState, error) {
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return ProjectCheckoutUnsafe, err
	}
	metadata, err := os.Lstat(filepath.Join(validated, ".git"))
	if err != nil || !metadata.IsDir() || metadata.Mode()&os.ModeSymlink != 0 {
		return ProjectCheckoutUnsafe, errors.New("project Git metadata is unavailable or unsafe")
	}
	gitPath, ok := findSafeLinuxTool("git", openCodeDefaultToolPath)
	if !ok {
		return ProjectCheckoutUnsafe, errors.New("git is unavailable")
	}
	runner := projectGitRunner{gitPath: gitPath}
	top, err := runner.run(ctx, validated, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(strings.TrimSpace(top)) != validated {
		return ProjectCheckoutUnsafe, errors.New("project checkout root is invalid")
	}
	remote, err := runner.run(ctx, validated, "remote", "get-url", "origin")
	if err != nil || !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return ProjectCheckoutRemoteMismatch, nil
	}
	pushRemote, err := runner.run(ctx, validated, "remote", "get-url", "--push", "origin")
	if err != nil || !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return ProjectCheckoutRemoteMismatch, nil
	}
	status, err := runner.run(ctx, validated, ProjectCheckoutStatusArgs()...)
	if err != nil {
		return ProjectCheckoutUnsafe, errors.New("project checkout status is unavailable")
	}
	if !ProjectCheckoutStatusClean(status) {
		return ProjectCheckoutDirty, nil
	}
	return ProjectCheckoutReady, nil
}

type projectGitRunner struct {
	gitPath        string
	commandTimeout time.Duration
	statusTimeout  time.Duration
}

func (r projectGitRunner) run(ctx context.Context, directory string, args ...string) (string, error) {
	timeout := r.commandTimeout
	if timeout <= 0 {
		timeout = projectGitCommandTimeout
	}
	if len(args) > 0 && args[0] == "status" {
		timeout = r.statusTimeout
		if timeout <= 0 {
			timeout = projectGitStatusTimeout
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, r.gitPath, args...)
	command.Dir = directory
	command.Env = []string{
		"PATH=" + openCodeDefaultToolPath,
		"HOME=/nonexistent",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=/dev/null",
		"GIT_CONFIG_KEY_1=core.fsmonitor",
		"GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=protocol.file.allow",
		"GIT_CONFIG_VALUE_2=never",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	capture := &boundedHTBLabCapture{limit: 64 << 10}
	command.Stdout = capture
	command.Stderr = capture
	if err := command.Run(); err != nil {
		return "", err
	}
	return capture.buffer.String(), nil
}
