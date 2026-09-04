//go:build windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func workspaceRootsPointer(roots WorkspaceRoots) *WorkspaceRoots {
	if strings.TrimSpace(roots.Dev) == "" && strings.TrimSpace(roots.HTBLinux) == "" && strings.TrimSpace(roots.WindowsDev) == "" {
		return nil
	}
	return &roots
}

// captureProjectProcessWorkspaceAttestation binds a durable process to the
// object identities opened from the registered Windows root. It intentionally
// excludes Git status and source contents, so normal development mutations do
// not invalidate a running process.
func captureProjectProcessWorkspaceAttestation(workspaceRoot, workspacePath, cwdPath string) (string, error) {
	return captureProjectProcessWorkspaceAttestationWithRoots(workspaceRoot, workspacePath, cwdPath, nil)
}

func captureProjectProcessWorkspaceAttestationWithRoots(workspaceRoot, workspacePath, cwdPath string, roots *WorkspaceRoots) (string, error) {
	if !IsWindowsLocalPath(workspaceRoot) || !IsWindowsLocalPath(workspacePath) || !IsWindowsLocalPath(cwdPath) ||
		!WindowsPathContained(workspaceRoot, workspacePath) || !WindowsPathContained(workspacePath, cwdPath) {
		return "", errors.New("project process workspace is unsafe")
	}
	workspace, err := OpenWindowsWorkcell(workspaceRoot, workspacePath)
	if err != nil {
		return "", errors.New("project process workspace is unavailable")
	}
	defer workspace.Close()
	cwd, err := OpenWindowsWorkcell(workspaceRoot, cwdPath)
	if err != nil {
		return "", errors.New("project process working directory is unavailable")
	}
	defer cwd.Close()
	gitIdentity := "none"
	gitPath := filepath.Join(workspace.FinalPath(), ".git")
	gitInfo, gitErr := os.Lstat(gitPath)
	switch {
	case gitErr == nil:
		if gitInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("project process Git metadata is unsafe")
		}
		gitIdentity, err = windowsGitAttestationIdentity(workspace.FinalPath(), gitPath, gitInfo, roots)
		if err != nil {
			return "", err
		}
	case errors.Is(gitErr, os.ErrNotExist):
	default:
		return "", errors.New("project process Git metadata is unavailable")
	}
	return hashProjectAttestation(
		"project-process-attestation-v1",
		"workspace="+windowsWorkspaceAttestationIdentity(workspace.Identity()),
		"cwd="+windowsWorkspaceAttestationIdentity(cwd.Identity()),
		"git="+gitIdentity,
	), nil
}

func revalidateProjectProcessWorkspaceAttestation(workspaceRoot, workspacePath, cwdPath, expected string) error {
	return revalidateProjectProcessWorkspaceAttestationWithRoots(workspaceRoot, workspacePath, cwdPath, expected, nil)
}

func revalidateProjectProcessWorkspaceAttestationWithRoots(workspaceRoot, workspacePath, cwdPath, expected string, roots *WorkspaceRoots) error {
	if strings.TrimSpace(expected) == "" {
		return errors.New("project process workspace attestation is missing")
	}
	observed, err := captureProjectProcessWorkspaceAttestationWithRoots(workspaceRoot, workspacePath, cwdPath, roots)
	if err != nil || observed != expected {
		return ErrProjectProcessIdentityChanged
	}
	return nil
}
