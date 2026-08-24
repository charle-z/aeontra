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

type windowsGitHubCommandRunner struct {
	stateRoot string
	toolPath  string
}

func NewGitHubCommandRunner(stateRoot, toolPath string) GitHubCommandRunner {
	return windowsGitHubCommandRunner{stateRoot: stateRoot, toolPath: toolPath}
}

func (runner windowsGitHubCommandRunner) Run(ctx context.Context, arguments []string, credential GitHubCredential) (string, error) {
	if ctx == nil || len(arguments) == 0 || credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(credential.Owner) || !validGitHubToken(credential.Token) {
		return "", errors.New("Windows GitHub request is invalid")
	}
	ghPath, err := resolveWindowsGitHubPath(runner.toolPath)
	if err != nil {
		return "", err
	}
	privateHome := filepath.Join(filepath.Clean(runner.stateRoot), "github-runtime")
	if err := preparePrivateRoot(privateHome); err != nil {
		return "", errors.New("GitHub runtime root is unsafe")
	}
	tempRoot := filepath.Join(privateHome, "tmp")
	if err := preparePrivateRoot(tempRoot); err != nil {
		return "", errors.New("GitHub runtime temporary root is unsafe")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output := &windowsGitCapture{limit: 256 << 10}
	command := exec.Command(ghPath, arguments...)
	command.Dir = privateHome
	command.Env = windowsGitHubEnvironment(runner.toolPath, privateHome, tempRoot, credential.Token)
	command.Stdout = output
	command.Stderr = output
	tree, err := NewWindowsProcessTree(WindowsProcessTreeLimits{MaxProcesses: 32, MemoryBytes: 512 << 20, CPUTime: 30 * time.Second, WallTime: 30 * time.Second})
	if err != nil {
		return "", errors.New("Windows GitHub process boundary unavailable")
	}
	defer tree.Close()
	if err := tree.Start(commandCtx, command); err != nil {
		return "", err
	}
	runErr := tree.Wait()
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	if output.truncated {
		return "", errors.New("GitHub CLI response exceeded the limit")
	}
	return redactDevGitCommandOutput(output.String(), credential.Token), runErr
}

func resolveWindowsGitHubPath(toolPath string) (string, error) {
	for _, candidate := range windowsToolCandidates(toolPath, "gh.exe", "GitHub CLI") {
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("GitHub CLI is unavailable")
}

func windowsToolCandidates(toolPath, executable, vendorDir string) []string {
	var candidates []string
	if trimmed := strings.TrimSpace(toolPath); trimmed != "" {
		candidates = append(candidates, filepath.Join(trimmed, executable))
	}
	programFiles := os.Getenv("ProgramFiles")
	if programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, vendorDir, executable))
	}
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	if programFilesX86 != "" {
		candidates = append(candidates, filepath.Join(programFilesX86, vendorDir, executable))
	}
	return candidates
}

func windowsGitHubEnvironment(toolPath, home, temp, token string) []string {
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
		"XDG_CONFIG_HOME=" + home,
		"TEMP=" + temp,
		"TMP=" + temp,
		"LANG=C",
		"LC_ALL=C",
		"GH_HOST=github.com",
		"GH_PROMPT_DISABLED=1",
		"GH_TOKEN=" + token,
	}
}
