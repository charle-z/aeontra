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
	if devGitFastForwardArguments(args) {
		return "", errors.New("Windows development Git fast-forward requires an isolated mutation runner")
	}
	remoteURL, network, err := devGitNetworkCommand(args, credential.Owner)
	if err != nil {
		return "", err
	}
	if network && (credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(credential.Owner) || !validGitHubToken(credential.Token)) {
		return "", errors.New("Windows development Git authority is unavailable")
	}
	gitPath, err := resolveWindowsGitPath(runner.toolPath)
	if err != nil {
		return "", err
	}
	secretRoot := filepath.Join(filepath.Clean(runner.stateRoot), "github-runtime")
	if err := preparePrivateRoot(secretRoot); err != nil {
		return "", errors.New("GitHub runtime root is unsafe")
	}
	var workspace *WindowsWorkspace
	commandDir := dir
	privateTransport := filepath.Dir(filepath.Clean(dir)) == secretRoot && strings.HasPrefix(filepath.Base(filepath.Clean(dir)), ".git-transport-")
	if privateTransport {
		if err := validatePrivateRoot(dir); err != nil {
			return "", errors.New("Windows Git transport root is unsafe")
		}
	} else {
		roots, err := DefaultWorkspaceRoots()
		if err != nil || roots.WindowsDev == "" {
			return "", errors.New("registered Windows Git root is unavailable")
		}
		workspace, err = OpenWindowsWorkcell(roots.WindowsDev, dir)
		if err != nil {
			return "", errors.New("Windows Git workspace is unsafe")
		}
		defer workspace.Close()
		if err := workspace.Revalidate(); err != nil {
			return "", err
		}
		commandDir = workspace.FinalPath()
	}
	tempRoot := filepath.Join(secretRoot, "tmp")
	if err := preparePrivateRoot(tempRoot); err != nil {
		return "", errors.New("GitHub runtime temporary root is unsafe")
	}
	askpassPath := ""
	if network {
		askpass, createErr := os.CreateTemp(secretRoot, ".askpass-*.cmd")
		if createErr != nil {
			return "", errors.New("GitHub askpass staging failed")
		}
		askpassPath = askpass.Name()
		defer os.Remove(askpassPath)
		if secureErr := securePrivateFile(askpass); secureErr != nil {
			_ = askpass.Close()
			return "", errors.New("GitHub askpass permissions failed")
		}
		const askpassScript = "@echo off\r\n" +
			"echo(%~1|%SystemRoot%\\System32\\findstr.exe /I \"Username\" >nul\r\n" +
			"if errorlevel 1 (set \"answer=%MCP_DEVBOX_GITHUB_TOKEN%\") else (set \"answer=x-access-token\")\r\n" +
			"<nul set /p \"=%answer%\"\r\nexit /b 0\r\n"
		if _, writeErr := askpass.WriteString(askpassScript); writeErr != nil {
			_ = askpass.Close()
			return "", errors.New("GitHub askpass staging failed")
		}
		if syncErr := askpass.Sync(); syncErr != nil {
			_ = askpass.Close()
			return "", errors.New("GitHub askpass staging failed")
		}
		if closeErr := askpass.Close(); closeErr != nil {
			return "", errors.New("GitHub askpass staging failed")
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output := &windowsGitCapture{limit: 1 << 20}
	command := exec.Command(gitPath, devGitProtectedArgumentsForOS(args, remoteURL, "NUL")...)
	if len(args) > 0 && (args[0] == "clone" || args[0] == "ls-remote") {
		commandDir = secretRoot
	}
	command.Dir = commandDir
	token := ""
	if network {
		token = credential.Token
	}
	command.Env = windowsGitEnvironment(runner.toolPath, secretRoot, tempRoot, askpassPath, token, devGitReadsLocalConfig(args))
	command.Stdout = output
	command.Stderr = output
	tree, err := NewWindowsProcessTree(WindowsProcessTreeLimits{MaxProcesses: 64, MemoryBytes: 1 << 30, CPUTime: 2 * time.Minute, WallTime: 2 * time.Minute})
	if err != nil {
		return "", errors.New("Windows Git process boundary unavailable")
	}
	defer tree.Close()
	if workspace != nil {
		if err := workspace.Revalidate(); err != nil {
			return "", err
		}
	}
	if err := tree.Start(commandCtx, command); err != nil {
		return "", err
	}
	runErr := tree.Wait()
	if workspace != nil {
		if revalidateErr := workspace.Revalidate(); revalidateErr != nil {
			return "", revalidateErr
		}
	}
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	return redactDevGitCommandOutput(output.String(), token), runErr
}

func (runner windowsDevGitCommandRunner) VerifyRemoteAncestor(ctx context.Context, dir, remoteURL, branch, remoteHead, head string, credential GitHubCredential) error {
	if !validDevGitBranch(branch) || !devGitCommitPattern.MatchString(remoteHead) || !devGitCommitPattern.MatchString(head) {
		return errors.New("Windows development Git ancestry binding is invalid")
	}
	transportRoot, cleanup, err := createDevGitTransportRoot(runner.stateRoot, dir)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := runner.Run(ctx, transportRoot, []string{"init", "--bare", "--quiet"}, credential); err != nil {
		return errors.New("Windows development Git transport initialization failed")
	}
	if err := configureDevGitAlternates(transportRoot, dir); err != nil {
		return err
	}
	if _, err := runner.Run(ctx, transportRoot, []string{"fetch", "--no-tags", remoteURL, "refs/heads/" + branch + ":refs/remotes/origin/" + branch}, credential); err != nil {
		return errors.New("Windows development remote state could not be fetched")
	}
	if _, err := runner.Run(ctx, transportRoot, []string{"merge-base", "--is-ancestor", remoteHead, head}, credential); err != nil {
		return errors.New("Windows development branch is behind or diverged")
	}
	return nil
}

func (runner windowsDevGitCommandRunner) PublishCommit(ctx context.Context, dir, remoteURL, head, branch string, credential GitHubCredential) (string, error) {
	if !validDevGitBranch(branch) || !devGitCommitPattern.MatchString(head) {
		return "", errors.New("Windows development Git publication binding is invalid")
	}
	transportRoot, cleanup, err := createDevGitTransportRoot(runner.stateRoot, dir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if _, err := runner.Run(ctx, transportRoot, []string{"init", "--bare", "--quiet"}, credential); err != nil {
		return "", errors.New("Windows development Git transport initialization failed")
	}
	if err := configureDevGitAlternates(transportRoot, dir); err != nil {
		return "", err
	}
	return runner.Run(ctx, transportRoot, []string{"push", "--porcelain", remoteURL, head + ":refs/heads/" + branch}, credential)
}

func windowsGitEnvironment(toolPath, home, temp, askpass, token string, readLocalConfig bool) []string {
	pathValue := strings.TrimSpace(toolPath)
	if pathValue == "" {
		pathValue = windowsGitSystemPath()
	}
	values := []string{
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"ComSpec=" + os.Getenv("ComSpec"),
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"PATH=" + pathValue,
		"HOME=" + home,
		"TEMP=" + temp,
		"TMP=" + temp,
		"LANG=C",
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=NUL",
		"GIT_CONFIG_SYSTEM=NUL",
		"GIT_OPTIONAL_LOCKS=0",
	}
	if !readLocalConfig {
		values = append(values, "GIT_CONFIG=NUL")
	}
	if token != "" {
		values = append(values, "GIT_ASKPASS="+askpass, "MCP_DEVBOX_GITHUB_TOKEN="+token)
	}
	return values
}

func redactDevGitCommandOutput(output, token string) string {
	if token == "" {
		return output
	}
	return strings.ReplaceAll(output, token, "[REDACTED]")
}
