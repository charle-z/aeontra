//go:build windows

package edgeclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func projectAttestationFingerprintPlatform(path string, roots *WorkspaceRoots) (string, error) {
	root, candidate, err := windowsAttestationRoot(path, roots)
	if err != nil {
		return "", err
	}
	workspaceHandle, err := OpenWindowsWorkspace(root, candidate)
	if err != nil {
		return "", errors.New("workspace attestation is unavailable")
	}
	validated := workspaceHandle.FinalPath()
	workspaceIdentity := windowsWorkspaceAttestationIdentity(workspaceHandle.identity)
	_ = workspaceHandle.Close()
	gitPath := filepath.Join(validated, ".git")
	gitInfo, err := os.Lstat(gitPath)
	if err != nil || gitInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Git attestation is unavailable")
	}
	gitIdentity, err := windowsGitAttestationIdentity(validated, gitPath, gitInfo, roots)
	if err != nil {
		return "", err
	}
	return hashProjectAttestation("project-attestation-v1", "workspace="+workspaceIdentity, "git="+gitIdentity), nil
}

func windowsAttestationRoot(path string, roots *WorkspaceRoots) (string, string, error) {
	candidate := filepath.Clean(strings.TrimSpace(path))
	if !IsWindowsLocalPath(candidate) {
		return "", "", errors.New("workspace attestation is unavailable")
	}
	if roots == nil {
		return candidate, candidate, nil
	}
	normalized, err := normalizeWorkspaceRoots(*roots)
	if err != nil {
		return "", "", errors.New("workspace attestation roots are unavailable")
	}
	for _, root := range []string{normalized.WindowsDev, normalized.Dev, normalized.HTBLinux} {
		if root == "" || !IsWindowsLocalPath(root) || strings.EqualFold(filepath.Clean(root), candidate) {
			continue
		}
		if WindowsPathContained(root, candidate) {
			return filepath.Clean(root), candidate, nil
		}
	}
	return "", "", errors.New("workspace attestation is outside authorized roots")
}

func windowsGitAttestationIdentity(workspacePath, gitPath string, gitInfo os.FileInfo, roots *WorkspaceRoots) (string, error) {
	metadataRoot, err := windowsMetadataRoot(workspacePath, roots)
	if err != nil {
		return "", err
	}
	identity, err := windowsObjectAttestationIdentity(metadataRoot, gitPath, gitInfo)
	if err != nil {
		return "", err
	}
	commonPath := gitPath
	if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
		return "", err
	}
	if gitInfo.Mode().IsRegular() {
		content, readErr := os.ReadFile(gitPath)
		if readErr != nil || len(content) == 0 || len(content) > 4<<10 {
			return "", errors.New("Git worktree attestation is unavailable")
		}
		line := strings.TrimSpace(string(content))
		if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
			return "", errors.New("Git worktree attestation is invalid")
		}
		commonPath = strings.TrimSpace(line[len("gitdir:"):])
		if !filepath.IsAbs(commonPath) {
			commonPath = filepath.Join(workspacePath, commonPath)
		}
		commonPath = filepath.Clean(commonPath)
		commonInfo, commonErr := os.Lstat(commonPath)
		if commonErr != nil || !commonInfo.IsDir() || commonInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("Git worktree attestation is unavailable")
		}
		if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
			return "", err
		}
		gitDirIdentity, identityErr := windowsObjectAttestationIdentity(metadataRoot, commonPath, commonInfo)
		if identityErr != nil {
			return "", identityErr
		}
		identity += "|gitdir=" + commonPath + "|gitdir_identity=" + gitDirIdentity
		resolvedCommonPath, hasCommonDir, commonErr := resolveGitCommonDirectory(commonPath)
		if commonErr != nil {
			return "", commonErr
		}
		if hasCommonDir {
			commonPath = resolvedCommonPath
			if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
				return "", err
			}
		}
	}
	commonInfo, commonErr := os.Lstat(commonPath)
	if commonErr != nil || !commonInfo.IsDir() || commonInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Git common directory attestation is unavailable")
	}
	if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
		return "", err
	}
	commonIdentity, identityErr := windowsObjectAttestationIdentity(metadataRoot, commonPath, commonInfo)
	if identityErr != nil {
		return "", identityErr
	}
	return identity + "|common=" + commonPath + "|common_identity=" + commonIdentity, nil
}

func windowsMetadataRoot(workspacePath string, roots *WorkspaceRoots) (string, error) {
	if roots == nil {
		return filepath.Clean(workspacePath), nil
	}
	normalized, err := normalizeWorkspaceRoots(*roots)
	if err != nil {
		return "", errors.New("Git metadata roots are unavailable")
	}
	workspacePath = filepath.Clean(workspacePath)
	for _, root := range []string{normalized.WindowsDev, normalized.Dev, normalized.HTBLinux} {
		if root != "" && IsWindowsLocalPath(root) && WindowsPathContained(root, workspacePath) {
			return filepath.Clean(root), nil
		}
	}
	return "", errors.New("Git metadata is outside authorized roots")
}

func validateManagedGitMetadataPath(path string, roots *WorkspaceRoots) error {
	if roots == nil {
		return nil
	}
	normalized, err := normalizeWorkspaceRoots(*roots)
	if err != nil {
		return errors.New("Git metadata roots are unavailable")
	}
	path = filepath.Clean(path)
	for _, root := range []string{normalized.WindowsDev, normalized.Dev, normalized.HTBLinux} {
		if root != "" && !strings.EqualFold(path, root) && WindowsPathContained(root, path) {
			if err := rejectSymlinkPath(path); err != nil {
				return errors.New("Git metadata path contains a symlink")
			}
			return nil
		}
	}
	return errors.New("Git metadata is outside authorized roots")
}

func windowsObjectAttestationIdentity(workspacePath, path string, info os.FileInfo) (string, error) {
	if info.Mode().IsDir() {
		object, err := OpenWindowsWorkspace(workspacePath, path)
		if err != nil {
			return "", errors.New("Windows Git object attestation is unavailable")
		}
		identity := windowsWorkspaceAttestationIdentity(object.identity)
		_ = object.Close()
		return identity, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("Windows Git object attestation is unavailable")
	}
	defer file.Close()
	identity, err := inspectWindowsHandle(file)
	if err != nil {
		return "", errors.New("Windows Git object attestation is unavailable")
	}
	return windowsWorkspaceAttestationIdentity(identity), nil
}

func windowsWorkspaceAttestationIdentity(identity WindowsWorkspaceIdentity) string {
	return fmt.Sprintf("volume=%d|index_high=%d|index_low=%d", identity.VolumeSerialNumber, identity.FileIndexHigh, identity.FileIndexLow)
}
