//go:build windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	projectWindowsGitCommandTimeout = 10 * time.Second
	projectWindowsGitStatusTimeout  = 2 * time.Minute
)

// windowsProjectCheckoutInspector performs inspection through a retained
// workcell handle. A caller-supplied path is only a lookup key; the handle
// identity and ACL are checked before and after every Git call.
type windowsProjectCheckoutInspector struct {
	root    string
	gitPath string
}

func newProjectCheckoutInspector() ProjectCheckoutInspector {
	roots, err := DefaultWorkspaceRoots()
	if err != nil || roots.WindowsDev == "" {
		return windowsProjectCheckoutInspector{}
	}
	gitPath, err := resolveWindowsGitPath("")
	if err != nil {
		return windowsProjectCheckoutInspector{root: roots.WindowsDev}
	}
	return windowsProjectCheckoutInspector{root: roots.WindowsDev, gitPath: gitPath}
}

func (inspector windowsProjectCheckoutInspector) Inspect(ctx context.Context, path, owner, repository string) (ProjectCheckoutState, error) {
	if inspector.root == "" || inspector.gitPath == "" {
		return ProjectCheckoutUnsafe, errors.New("registered Windows workcell or Git is unavailable")
	}
	workspace, err := OpenWindowsWorkcell(inspector.root, path)
	if err != nil {
		return ProjectCheckoutUnsafe, err
	}
	defer workspace.Close()
	if err := workspace.Revalidate(); err != nil {
		return ProjectCheckoutUnsafe, err
	}
	metadata, err := os.Lstat(filepath.Join(workspace.FinalPath(), ".git"))
	if err != nil || metadata.Mode()&os.ModeSymlink != 0 || (!metadata.IsDir() && !metadata.Mode().IsRegular()) {
		return ProjectCheckoutUnsafe, errors.New("project Git metadata is unavailable or unsafe")
	}
	if metadata.Mode().IsRegular() {
		if err := validateWindowsWorktreeGitFile(inspector.root, workspace.FinalPath()); err != nil {
			return ProjectCheckoutUnsafe, err
		}
	}
	runner := windowsProjectGitRunner{gitPath: inspector.gitPath}
	run := func(args ...string) (string, error) {
		if err := workspace.Revalidate(); err != nil {
			return "", err
		}
		output, runErr := runner.run(ctx, workspace, args...)
		if revalidateErr := workspace.Revalidate(); revalidateErr != nil {
			return "", revalidateErr
		}
		return output, runErr
	}
	top, err := run("rev-parse", "--show-toplevel")
	if err != nil || !strings.EqualFold(filepath.Clean(strings.TrimSpace(top)), filepath.Clean(workspace.FinalPath())) {
		return ProjectCheckoutUnsafe, errors.New("project checkout root is invalid")
	}
	remote, err := run("remote", "get-url", "origin")
	if err != nil || !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return ProjectCheckoutRemoteMismatch, nil
	}
	pushRemote, err := run("remote", "get-url", "--push", "origin")
	if err != nil || !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return ProjectCheckoutRemoteMismatch, nil
	}
	status, err := run(ProjectCheckoutStatusArgs()...)
	if err != nil {
		return ProjectCheckoutUnsafe, errors.New("project checkout status is unavailable")
	}
	if !ProjectCheckoutStatusClean(status) {
		return ProjectCheckoutDirty, nil
	}
	return ProjectCheckoutReady, nil
}

func validateWindowsWorktreeGitFile(root, checkout string) error {
	content, err := os.ReadFile(filepath.Join(checkout, ".git"))
	if err != nil || len(content) == 0 || len(content) > 4<<10 {
		return errors.New("project Git worktree metadata is invalid")
	}
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
		return errors.New("project Git worktree metadata is invalid")
	}
	gitDir := strings.TrimSpace(line[len("gitdir:"):])
	if gitDir == "" {
		return errors.New("project Git worktree metadata is invalid")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(checkout, gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	if !WindowsPathContained(root, gitDir) {
		return errors.New("project Git worktree metadata escaped its root")
	}
	metadata, err := os.Lstat(gitDir)
	if err != nil || !metadata.IsDir() || metadata.Mode()&os.ModeSymlink != 0 {
		return errors.New("project Git worktree metadata is unavailable")
	}
	worktree, err := OpenWindowsWorkcell(root, gitDir)
	if err != nil {
		return errors.New("project Git worktree metadata is unsafe")
	}
	defer worktree.Close()
	return worktree.Revalidate()
}

type windowsProjectGitRunner struct {
	gitPath        string
	commandTimeout time.Duration
	statusTimeout  time.Duration
}

func (runner windowsProjectGitRunner) run(ctx context.Context, workspace *WindowsWorkspace, args ...string) (string, error) {
	if workspace == nil || workspace.File() == nil || runner.gitPath == "" {
		return "", errors.New("Windows Git runner is unavailable")
	}
	timeout := runner.commandTimeout
	if timeout <= 0 {
		timeout = projectWindowsGitCommandTimeout
	}
	if len(args) > 0 && args[0] == "status" {
		timeout = runner.statusTimeout
		if timeout <= 0 {
			timeout = projectWindowsGitStatusTimeout
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output := &windowsGitCapture{limit: 64 << 10}
	command := exec.Command(runner.gitPath, args...)
	command.Dir = workspace.FinalPath()
	command.Env = windowsGitInspectionEnvironmentWithPath(windowsGitPathEnvironment(runner.gitPath))
	command.Stdout = output
	command.Stderr = output
	tree, err := NewWindowsProcessTree(windowsGitProcessTreeLimits())
	if err != nil {
		return "", errors.New("Windows Git process boundary unavailable")
	}
	defer tree.Close()
	if err := tree.Start(commandCtx, command); err != nil {
		return "", err
	}
	err = tree.Wait()
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	return output.String(), err
}

func windowsGitProcessTreeLimits() WindowsProcessTreeLimits {
	return WindowsProcessTreeLimits{MaxProcesses: 32, MemoryBytes: 512 << 20, CPUTime: 2 * time.Minute, WallTime: projectWindowsGitStatusTimeout}
}

func windowsGitInspectionEnvironment() []string {
	return windowsGitInspectionEnvironmentWithPath(windowsGitSystemPath())
}

func windowsGitInspectionEnvironmentWithPath(pathValue string) []string {
	return []string{
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"ComSpec=" + os.Getenv("ComSpec"),
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"PATH=" + pathValue,
		"TEMP=" + os.TempDir(),
		"TMP=" + os.TempDir(),
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=NUL",
		"GIT_CONFIG_SYSTEM=NUL",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=NUL",
		"GIT_CONFIG_KEY_1=core.fsmonitor",
		"GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=protocol.file.allow",
		"GIT_CONFIG_VALUE_2=never",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	}
}

func resolveWindowsGitPath(toolPath string) (string, error) {
	pathValue := strings.TrimSpace(toolPath)
	if pathValue == "" {
		programFiles := os.Getenv("ProgramFiles")
		pathValue = filepath.Join(programFiles, "Git", "cmd") + string(os.PathListSeparator) + filepath.Join(programFiles, "Git", "bin")
	}
	for _, directory := range filepath.SplitList(pathValue) {
		directory = filepath.Clean(strings.TrimSpace(directory))
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		candidate := filepath.Join(directory, "git.exe")
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("Git executable is unavailable")
}

func windowsGitSystemPath() string {
	programFiles := os.Getenv("ProgramFiles")
	return filepath.Join(programFiles, "Git", "cmd") + string(os.PathListSeparator) + filepath.Join(programFiles, "Git", "bin") + string(os.PathListSeparator) + filepath.Join(os.Getenv("SystemRoot"), "System32")
}

func windowsGitPathEnvironment(gitPath string) string {
	if gitPath == "" {
		return windowsGitSystemPath()
	}
	return filepath.Dir(gitPath) + string(os.PathListSeparator) + filepath.Join(os.Getenv("SystemRoot"), "System32")
}

type windowsGitCapture struct {
	mu        sync.Mutex
	value     strings.Builder
	limit     int
	truncated bool
}

func (capture *windowsGitCapture) Write(value []byte) (int, error) {
	if capture == nil || capture.limit <= 0 {
		return len(value), nil
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	remaining := capture.limit - capture.value.Len()
	if remaining <= 0 {
		capture.truncated = true
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = capture.value.Write(value[:remaining])
		capture.truncated = true
		return len(value), nil
	}
	_, _ = capture.value.Write(value)
	return len(value), nil
}

func (capture *windowsGitCapture) String() string {
	if capture == nil {
		return ""
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.value.String()
}
