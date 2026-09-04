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

func newProjectCheckoutInspectorWithRoots(roots WorkspaceRoots) ProjectCheckoutInspector {
	gitPath, err := resolveWindowsGitPath("")
	if err != nil || roots.WindowsDev == "" {
		return windowsProjectCheckoutInspector{root: roots.WindowsDev}
	}
	return windowsProjectCheckoutInspector{root: roots.WindowsDev, gitPath: gitPath}
}

func (inspector windowsProjectCheckoutInspector) Inspect(ctx context.Context, path, owner, repository string) (ProjectCheckoutState, error) {
	observation, err := inspector.InspectDetailed(ctx, path, owner, repository)
	if err != nil {
		return ProjectCheckoutUnavailable, err
	}
	switch observation.State {
	case ProjectCheckoutIdentityMismatch:
		return ProjectCheckoutRemoteMismatch, nil
	case ProjectCheckoutUnsafeBoundary, ProjectCheckoutCorrupt, ProjectCheckoutUnavailable, ProjectCheckoutTimeout:
		return ProjectCheckoutUnsafe, errors.New(observation.Diagnostic.Reason)
	default:
		return observation.State, nil
	}
}

// InspectRepositoryIdentity is the bounded Git-only observation used by
// project_registry_list. It keeps the Windows handle/ACL checks while
// avoiding status and arbitrary filesystem discovery.
func (inspector windowsProjectCheckoutInspector) InspectRepositoryIdentity(ctx context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	if inspector.root == "" || inspector.gitPath == "" {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnavailable, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_unavailable", Path: path, Repairable: false,
		}}, nil
	}
	workspace, err := OpenWindowsWorkcell(inspector.root, path)
	if err != nil {
		return classifyWindowsWorkspaceCheckout(path, err), nil
	}
	defer workspace.Close()
	if err := workspace.Revalidate(); err != nil {
		return classifyWindowsWorkspaceCheckout(path, err), nil
	}
	metadataPath := filepath.Join(workspace.FinalPath(), ".git")
	metadata, err := os.Lstat(metadataPath)
	if err != nil {
		return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_metadata_missing_or_invalid", Path: metadataPath, Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	if metadata.Mode()&os.ModeSymlink != 0 || (!metadata.IsDir() && !metadata.Mode().IsRegular()) {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnsafeBoundary, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "workspace_boundary_violation", Path: metadataPath, Repairable: false,
		}}, nil
	}
	if metadata.Mode().IsRegular() {
		if err := validateWindowsWorktreeGitFile(inspector.root, workspace.FinalPath()); err != nil {
			return classifyWindowsMetadataCheckout(workspace.FinalPath(), err), nil
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
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_root_inspection", err), nil
	}
	if !strings.EqualFold(filepath.Clean(strings.TrimSpace(top)), filepath.Clean(workspace.FinalPath())) {
		return ProjectCheckoutObservation{State: ProjectCheckoutIdentityMismatch, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "repository_root_mismatch", Path: workspace.FinalPath(), Expected: workspace.FinalPath(), Observed: sanitizeCheckoutValue(top), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	remote, err := run("remote", "get-url", "origin")
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_fetch_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return repositoryMismatchObservation(workspace.FinalPath(), owner, repository, remote), nil
	}
	pushRemote, err := run("remote", "get-url", "--push", "origin")
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_push_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return repositoryMismatchObservation(workspace.FinalPath(), owner, repository, pushRemote), nil
	}
	return ProjectCheckoutObservation{State: ProjectCheckoutReady, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "checkout_repository_identity_verified", Path: workspace.FinalPath(), Repairable: false,
	}}, nil
}

func (inspector windowsProjectCheckoutInspector) InspectDetailed(ctx context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	if inspector.root == "" || inspector.gitPath == "" {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnavailable, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_unavailable", Path: path, Repairable: false,
		}}, nil
	}
	workspace, err := OpenWindowsWorkcell(inspector.root, path)
	if err != nil {
		return classifyWindowsWorkspaceCheckout(path, err), nil
	}
	defer workspace.Close()
	if err := workspace.Revalidate(); err != nil {
		return classifyWindowsWorkspaceCheckout(path, err), nil
	}
	metadata, err := os.Lstat(filepath.Join(workspace.FinalPath(), ".git"))
	if err != nil {
		return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_metadata_missing_or_invalid", Path: filepath.Join(workspace.FinalPath(), ".git"), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	if metadata.Mode()&os.ModeSymlink != 0 || (!metadata.IsDir() && !metadata.Mode().IsRegular()) {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnsafeBoundary, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "workspace_boundary_violation", Path: filepath.Join(workspace.FinalPath(), ".git"), Repairable: false,
		}}, nil
	}
	if metadata.Mode().IsRegular() {
		if err := validateWindowsWorktreeGitFile(inspector.root, workspace.FinalPath()); err != nil {
			return classifyWindowsMetadataCheckout(workspace.FinalPath(), err), nil
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
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_root_inspection", err), nil
	}
	if !strings.EqualFold(filepath.Clean(strings.TrimSpace(top)), filepath.Clean(workspace.FinalPath())) {
		return ProjectCheckoutObservation{State: ProjectCheckoutIdentityMismatch, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "repository_root_mismatch", Path: workspace.FinalPath(), Expected: workspace.FinalPath(), Observed: sanitizeCheckoutValue(top), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	remote, err := run("remote", "get-url", "origin")
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_fetch_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return repositoryMismatchObservation(workspace.FinalPath(), owner, repository, remote), nil
	}
	pushRemote, err := run("remote", "get-url", "--push", "origin")
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_push_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return repositoryMismatchObservation(workspace.FinalPath(), owner, repository, pushRemote), nil
	}
	status, err := run(ProjectCheckoutStatusArgs()...)
	if err != nil {
		return classifyWindowsGitFailure(ctx, workspace.FinalPath(), "git_status", err), nil
	}
	if !ProjectCheckoutStatusClean(status) {
		return ProjectCheckoutObservation{State: ProjectCheckoutDirty, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "normal_workspace_changes", Path: workspace.FinalPath(), Repairable: false,
		}}, nil
	}
	return ProjectCheckoutObservation{State: ProjectCheckoutReady, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "checkout_ready", Path: workspace.FinalPath(), Repairable: false,
	}}, nil
}

func classifyWindowsWorkspaceCheckout(path string, err error) ProjectCheckoutObservation {
	state := ProjectCheckoutUnavailable
	reason := "workspace_unavailable"
	if errors.Is(err, ErrWindowsWorkspaceUnsafe) || errors.Is(err, ErrWindowsWorkspaceEscaped) || errors.Is(err, ErrWindowsWorkspaceReplaced) || errors.Is(err, ErrWindowsWorkspaceACLUnsafe) {
		state = ProjectCheckoutUnsafeBoundary
		reason = "workspace_boundary_violation"
	}
	return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: reason, Path: sanitizeCheckoutValue(path), Repairable: state != ProjectCheckoutUnsafeBoundary, RecommendedAction: "project_reconcile",
	}}
}

func classifyWindowsMetadataCheckout(path string, err error) ProjectCheckoutObservation {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "escaped") || strings.Contains(message, "unsafe") || strings.Contains(message, "replaced") {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnsafeBoundary, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "workspace_boundary_violation", Path: filepath.Join(path, ".git"), Repairable: false,
		}}
	}
	return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "git_metadata_invalid", Path: filepath.Join(path, ".git"), Repairable: true, RecommendedAction: "project_reconcile",
	}}
}

func classifyWindowsGitFailure(ctx context.Context, path, operation string, err error) ProjectCheckoutObservation {
	state := ProjectCheckoutUnavailable
	reason := operation + "_unavailable"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		state = ProjectCheckoutTimeout
		reason = operation + "_timeout"
	}
	if operation == "git_root_inspection" && state != ProjectCheckoutTimeout {
		state = ProjectCheckoutCorrupt
		reason = "git_repository_invalid"
	}
	return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: reason, Path: sanitizeCheckoutValue(path), Repairable: true, RecommendedAction: "project_reconcile",
	}}
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
