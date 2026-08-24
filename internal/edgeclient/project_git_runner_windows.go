//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type windowsDevGitCommandRunner struct {
	stateRoot string
	toolPath  string
}

func NewDevGitCommandRunner(stateRoot, toolPath string) DevGitCommandRunner {
	return windowsDevGitCommandRunner{stateRoot: stateRoot, toolPath: toolPath}
}

func (runner windowsDevGitCommandRunner) Run(ctx context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	if ctx == nil || dir == "" || len(args) == 0 {
		return "", errors.New("Windows development Git request is invalid")
	}
	if windowsGitCommandNeedsCredential(args) && (credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(credential.Owner) || !validGitHubToken(credential.Token)) {
		return "", errors.New("Windows development Git authority is unavailable")
	}
	gitPath, err := resolveWindowsGitPath(runner.toolPath)
	if err != nil {
		return "", err
	}
	roots, err := DefaultWorkspaceRoots()
	if err != nil || roots.WindowsDev == "" {
		return "", errors.New("registered Windows Git root is unavailable")
	}
	workspace, err := OpenWindowsWorkcell(roots.WindowsDev, dir)
	if err != nil {
		return "", errors.New("Windows Git workspace is unsafe")
	}
	defer workspace.Close()
	if err := workspace.Revalidate(); err != nil {
		return "", err
	}
	secretRoot := filepath.Join(filepath.Clean(runner.stateRoot), "github-runtime")
	if err := preparePrivateRoot(secretRoot); err != nil {
		return "", errors.New("GitHub runtime root is unsafe")
	}
	tempRoot := filepath.Join(secretRoot, "tmp")
	if err := preparePrivateRoot(tempRoot); err != nil {
		return "", errors.New("GitHub runtime temporary root is unsafe")
	}
	askpass, err := os.CreateTemp(secretRoot, ".askpass-*.cmd")
	if err != nil {
		return "", errors.New("GitHub askpass staging failed")
	}
	askpassPath := askpass.Name()
	defer os.Remove(askpassPath)
	if err := securePrivateFile(askpass); err != nil {
		_ = askpass.Close()
		return "", errors.New("GitHub askpass permissions failed")
	}
	const askpassScript = "@echo off\r\n" +
		"echo(%~1|%SystemRoot%\\System32\\findstr.exe /I \"Username\" >nul\r\n" +
		"if errorlevel 1 (set \"answer=%MCP_DEVBOX_GITHUB_TOKEN%\") else (set \"answer=x-access-token\")\r\n" +
		"<nul set /p \"=%answer%\"\r\nexit /b 0\r\n"
	if _, err := askpass.WriteString(askpassScript); err != nil {
		_ = askpass.Close()
		return "", errors.New("GitHub askpass staging failed")
	}
	if err := askpass.Sync(); err != nil {
		_ = askpass.Close()
		return "", errors.New("GitHub askpass staging failed")
	}
	if err := askpass.Close(); err != nil {
		return "", errors.New("GitHub askpass staging failed")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output := &windowsGitCapture{limit: 1 << 20}
	command := exec.Command(gitPath, args...)
	command.Dir = workspace.FinalPath()
	command.Env = windowsGitEnvironment(runner.toolPath, secretRoot, tempRoot, askpassPath, credential.Token)
	command.Stdout = output
	command.Stderr = output
	tree, err := NewWindowsProcessTree(WindowsProcessTreeLimits{MaxProcesses: 64, MemoryBytes: 1 << 30, CPUTime: 2 * time.Minute, WallTime: 2 * time.Minute})
	if err != nil {
		return "", errors.New("Windows Git process boundary unavailable")
	}
	defer tree.Close()
	if err := workspace.Revalidate(); err != nil {
		return "", err
	}
	if err := tree.Start(commandCtx, command); err != nil {
		return "", err
	}
	runErr := tree.Wait()
	if revalidateErr := workspace.Revalidate(); revalidateErr != nil {
		return "", revalidateErr
	}
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	return redactDevGitCommandOutput(output.String(), credential.Token), runErr
}

func windowsGitCommandNeedsCredential(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "fetch", "push", "pull", "clone", "ls-remote", "submodule":
		return true
	default:
		return false
	}
}

func windowsGitEnvironment(toolPath, home, temp, askpass, token string) []string {
	pathValue := strings.TrimSpace(toolPath)
	if pathValue == "" {
		pathValue = windowsGitSystemPath()
	}
	return []string{
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"ComSpec=" + os.Getenv("ComSpec"),
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"PATH=" + pathValue,
		"HOME=" + home,
		"TEMP=" + temp,
		"TMP=" + temp,
		"LANG=C",
		"LC_ALL=C",
		"GIT_ASKPASS=" + askpass,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=NUL",
		"GIT_CONFIG_SYSTEM=NUL",
		"GIT_CONFIG_COUNT=4",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=core.hooksPath",
		"GIT_CONFIG_VALUE_1=NUL",
		"GIT_CONFIG_KEY_2=core.fsmonitor",
		"GIT_CONFIG_VALUE_2=false",
		"GIT_CONFIG_KEY_3=protocol.file.allow",
		"GIT_CONFIG_VALUE_3=never",
		"GIT_OPTIONAL_LOCKS=0",
		"MCP_DEVBOX_GITHUB_TOKEN=" + token,
	}
}

func redactDevGitCommandOutput(output, token string) string {
	if token == "" {
		return output
	}
	return strings.ReplaceAll(output, token, "[REDACTED]")
}
