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

type localProjectCheckoutInspector struct {
	roots *WorkspaceRoots
}

func newProjectCheckoutInspector() ProjectCheckoutInspector {
	return localProjectCheckoutInspector{}
}

func newProjectCheckoutInspectorWithRoots(roots WorkspaceRoots) ProjectCheckoutInspector {
	return localProjectCheckoutInspector{roots: &roots}
}

func (inspector localProjectCheckoutInspector) Inspect(ctx context.Context, path, owner, repository string) (ProjectCheckoutState, error) {
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

// InspectRepositoryIdentity performs the bounded subset used by registry
// listing. It deliberately omits git status so a normal dirty workspace or a
// long-running build cannot turn a read-only claim listing into discovery.
func (inspector localProjectCheckoutInspector) InspectRepositoryIdentity(ctx context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return classifyWorkspaceCheckout(path, err), nil
	}
	metadataPath := filepath.Join(validated, ".git")
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
		if err := validateLinuxProjectGitPointer(validated, metadataPath, metadata, inspector.roots); err != nil {
			message := strings.ToLower(err.Error())
			state := ProjectCheckoutCorrupt
			reason := "git_metadata_invalid"
			if strings.Contains(message, "unsafe") || strings.Contains(message, "symlink") || strings.Contains(message, "boundary") {
				state = ProjectCheckoutUnsafeBoundary
				reason = "workspace_boundary_violation"
			}
			return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
				Reason: reason, Path: metadataPath, Repairable: state != ProjectCheckoutUnsafeBoundary, RecommendedAction: "project_reconcile",
			}}, nil
		}
	}
	gitPath, ok := findSafeLinuxTool("git", openCodeDefaultToolPath)
	if !ok {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnavailable, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_unavailable", Path: validated, Repairable: false,
		}}, nil
	}
	runner := projectGitRunner{gitPath: gitPath}
	top, err := runner.run(ctx, validated, "rev-parse", "--show-toplevel")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_root_inspection", err), nil
	}
	if filepath.Clean(strings.TrimSpace(top)) != validated {
		return ProjectCheckoutObservation{State: ProjectCheckoutIdentityMismatch, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "repository_root_mismatch", Path: validated, Expected: validated, Observed: sanitizeCheckoutValue(top), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	remote, err := runner.run(ctx, validated, "remote", "get-url", "origin")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_fetch_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return repositoryMismatchObservation(validated, owner, repository, remote), nil
	}
	pushRemote, err := runner.run(ctx, validated, "remote", "get-url", "--push", "origin")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_push_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return repositoryMismatchObservation(validated, owner, repository, pushRemote), nil
	}
	return ProjectCheckoutObservation{State: ProjectCheckoutReady, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "checkout_repository_identity_verified", Path: validated, Repairable: false,
	}}, nil
}

func (inspector localProjectCheckoutInspector) InspectDetailed(ctx context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return classifyWorkspaceCheckout(path, err), nil
	}
	metadata, err := os.Lstat(filepath.Join(validated, ".git"))
	if err != nil {
		return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_metadata_missing_or_invalid", Path: filepath.Join(validated, ".git"), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	if metadata.Mode()&os.ModeSymlink != 0 {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnsafeBoundary, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "workspace_boundary_violation", Path: filepath.Join(validated, ".git"), Repairable: false,
		}}, nil
	}
	if !metadata.IsDir() && !metadata.Mode().IsRegular() {
		return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_metadata_invalid", Path: filepath.Join(validated, ".git"), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	if metadata.Mode().IsRegular() {
		if err := validateLinuxProjectGitPointer(validated, filepath.Join(validated, ".git"), metadata, inspector.roots); err != nil {
			message := strings.ToLower(err.Error())
			state := ProjectCheckoutCorrupt
			reason := "git_metadata_invalid"
			if strings.Contains(message, "unsafe") || strings.Contains(message, "symlink") || strings.Contains(message, "boundary") {
				state = ProjectCheckoutUnsafeBoundary
				reason = "workspace_boundary_violation"
			}
			return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
				Reason: reason, Path: filepath.Join(validated, ".git"), Repairable: state != ProjectCheckoutUnsafeBoundary, RecommendedAction: "project_reconcile",
			}}, nil
		}
	}
	gitPath, ok := findSafeLinuxTool("git", openCodeDefaultToolPath)
	if !ok {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnavailable, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "git_unavailable", Path: validated, Repairable: false,
		}}, nil
	}
	runner := projectGitRunner{gitPath: gitPath}
	top, err := runner.run(ctx, validated, "rev-parse", "--show-toplevel")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_root_inspection", err), nil
	}
	if filepath.Clean(strings.TrimSpace(top)) != validated {
		return ProjectCheckoutObservation{State: ProjectCheckoutIdentityMismatch, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "repository_root_mismatch", Path: validated, Expected: validated, Observed: sanitizeCheckoutValue(top), Repairable: true, RecommendedAction: "project_reconcile",
		}}, nil
	}
	remote, err := runner.run(ctx, validated, "remote", "get-url", "origin")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_fetch_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(remote), owner, repository) {
		return repositoryMismatchObservation(validated, owner, repository, remote), nil
	}
	pushRemote, err := runner.run(ctx, validated, "remote", "get-url", "--push", "origin")
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_push_remote_inspection", err), nil
	}
	if !projectRemoteMatches(strings.TrimSpace(pushRemote), owner, repository) {
		return repositoryMismatchObservation(validated, owner, repository, pushRemote), nil
	}
	status, err := runner.run(ctx, validated, ProjectCheckoutStatusArgs()...)
	if err != nil {
		return classifyGitFailure(ctx, validated, "git_status", err), nil
	}
	if !ProjectCheckoutStatusClean(status) {
		return ProjectCheckoutObservation{State: ProjectCheckoutDirty, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "normal_workspace_changes", Path: validated, Repairable: false,
		}}, nil
	}
	return ProjectCheckoutObservation{State: ProjectCheckoutReady, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: "checkout_ready", Path: validated, Repairable: false,
	}}, nil
}

func validateLinuxProjectGitPointer(checkout, gitPath string, info os.FileInfo, roots *WorkspaceRoots) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || !ownedByCurrentUIDPortable(info) {
		return errors.New("Git worktree metadata is unsafe")
	}
	if _, err := projectGitAttestationIdentity(checkout, gitPath, info, roots); err != nil {
		return err
	}
	return nil
}

func classifyWorkspaceCheckout(path string, err error) ProjectCheckoutObservation {
	reason := "workspace_unavailable"
	state := ProjectCheckoutUnavailable
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "unsafe") || strings.Contains(message, "symlink") || strings.Contains(message, "owned") || strings.Contains(message, "mount") {
		state = ProjectCheckoutUnsafeBoundary
		reason = "workspace_boundary_violation"
	}
	return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: reason, Path: sanitizeCheckoutValue(path), Observed: sanitizeCheckoutValue(err.Error()),
		Repairable: state != ProjectCheckoutUnsafeBoundary, RecommendedAction: "project_reconcile",
	}}
}

func classifyGitFailure(ctx context.Context, path, operation string, err error) ProjectCheckoutObservation {
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
		Reason: reason, Path: sanitizeCheckoutValue(path), Observed: sanitizeCheckoutValue(err.Error()),
		Repairable: true, RecommendedAction: "project_reconcile",
	}}
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
		if commandCtx.Err() != nil {
			return "", commandCtx.Err()
		}
		return "", err
	}
	return capture.buffer.String(), nil
}
