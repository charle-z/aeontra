//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type execGitHubCommandRunner struct {
	stateRoot string
	toolPath  string
}

func NewGitHubCommandRunner(stateRoot, toolPath string) GitHubCommandRunner {
	return execGitHubCommandRunner{stateRoot: stateRoot, toolPath: toolPath}
}

func (runner execGitHubCommandRunner) Run(ctx context.Context, arguments []string, credential GitHubCredential) (string, error) {
	ghPath, ok := findSafeLinuxTool("gh", runner.toolPath)
	if !ok {
		return "", errors.New("GitHub CLI is unavailable")
	}
	privateHome := filepath.Join(runner.stateRoot, "github-runtime")
	if err := preparePrivateRoot(privateHome); err != nil {
		return "", errors.New("GitHub runtime root is unsafe")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, ghPath, arguments...)
	command.Dir = privateHome
	command.Env = []string{
		"PATH=" + runner.toolPath, "HOME=" + privateHome, "XDG_CONFIG_HOME=" + privateHome,
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "GH_HOST=github.com", "GH_PROMPT_DISABLED=1", "GH_TOKEN=" + credential.Token,
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 2 * time.Second
	output := &boundedHTBLabCapture{limit: 256 << 10}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	text := strings.ReplaceAll(output.buffer.String(), credential.Token, "[REDACTED]")
	if output.truncated {
		return "", errors.New("GitHub CLI response exceeded the limit")
	}
	return text, err
}
