//go:build !windows

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

// captureProjectProcessWorkspaceAttestation records only identities which
// must remain stable between workcell preparation and spawn.  It deliberately
// does not inspect Git status or source contents: normal edits and build
// outputs are allowed while a process is running.
func captureProjectProcessWorkspaceAttestation(workspacePath, cwdPath string) (string, error) {
	return captureProjectProcessWorkspaceAttestationWithRoots(workspacePath, cwdPath, nil)
}

func captureProjectProcessWorkspaceAttestationWithRoots(workspacePath, cwdPath string, roots *WorkspaceRoots) (string, error) {
	workspace, err := ValidateRegisteredWorkspace(workspacePath)
	if err != nil || filepath.Clean(workspace) != filepath.Clean(workspacePath) {
		return "", errors.New("project process workspace is unavailable")
	}
	workspaceInfo, err := os.Lstat(workspace)
	if err != nil || !workspaceInfo.IsDir() || workspaceInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project process workspace is unsafe")
	}
	cwdIdentity, err := projectProcessDirectoryIdentity(workspace, cwdPath)
	if err != nil {
		return "", err
	}
	gitIdentity := "none"
	gitPath := filepath.Join(workspace, ".git")
	gitInfo, gitErr := os.Lstat(gitPath)
	switch {
	case gitErr == nil:
		if gitInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("project process Git metadata is unsafe")
		}
		gitIdentity, err = projectGitAttestationIdentity(workspace, gitPath, gitInfo, roots)
		if err != nil {
			return "", err
		}
	case errors.Is(gitErr, os.ErrNotExist):
		// Process tests and non-Git workspaces are valid. The workspace object
		// identity still prevents a path swap.
	default:
		return "", errors.New("project process Git metadata is unavailable")
	}
	return hashProjectAttestation(
		"project-process-attestation-v1",
		"workspace="+fileAttestationIdentity(workspaceInfo),
		"cwd="+cwdIdentity,
		"git="+gitIdentity,
	), nil
}

func revalidateProjectProcessWorkspaceAttestation(workspacePath, cwdPath, expected string) error {
	return revalidateProjectProcessWorkspaceAttestationWithRoots(workspacePath, cwdPath, expected, nil)
}

func revalidateProjectProcessWorkspaceAttestationWithRoots(workspacePath, cwdPath, expected string, roots *WorkspaceRoots) error {
	if strings.TrimSpace(expected) == "" {
		return errors.New("project process workspace attestation is missing")
	}
	observed, err := captureProjectProcessWorkspaceAttestationWithRoots(workspacePath, cwdPath, roots)
	if err != nil || observed != expected {
		return ErrProjectProcessIdentityChanged
	}
	return nil
}

func projectProcessDirectoryIdentity(workspacePath, path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || !filepath.IsAbs(path) || !pathInside(workspacePath, path) && filepath.Clean(path) != filepath.Clean(workspacePath) {
		return "", errors.New("project process working directory is unsafe")
	}
	if err := rejectSymlinkPath(path); err != nil {
		return "", errors.New("project process working directory is unsafe")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project process working directory is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != path {
		return "", errors.New("project process working directory is unsafe")
	}
	return fileAttestationIdentity(info), nil
}
