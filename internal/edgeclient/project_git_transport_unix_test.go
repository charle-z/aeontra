//go:build !windows

package edgeclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevGitNetworkCommandIgnoresRepositoryURLRewrite(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	repository := t.TempDir()
	initCommand := exec.Command(gitPath, "init", "--quiet", repository)
	initCommand.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := initCommand.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	remote := "https://github.com/charle-z/project.git"
	configCommand := exec.Command(gitPath, "-C", repository, "config", "--local", "url.file:///tmp/attacker.insteadOf", remote)
	configCommand.Env = initCommand.Env
	if output, err := configCommand.CombinedOutput(); err != nil {
		t.Fatalf("write hostile local config: %v: %s", err, output)
	}

	helperRoot := t.TempDir()
	helper := filepath.Join(helperRoot, "git-remote-https")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf 'effective-url=%s\\n' \"$2\" >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(gitPath, devGitProtectedArguments([]string{"ls-remote", "--heads", remote, "refs/heads/main"}, remote)...)
	// Remote inspection runs from the private broker root, never from the
	// repository whose local config is controlled by the workcell.
	command.Dir = helperRoot
	command.Env = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + t.TempDir(),
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EXEC_PATH=" + helperRoot,
	}
	output, _ := command.CombinedOutput()
	if !strings.Contains(string(output), "effective-url="+remote) {
		t.Fatalf("repository-local URL rewrite changed the network target: %s", output)
	}
}
