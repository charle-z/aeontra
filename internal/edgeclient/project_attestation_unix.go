//go:build !windows

package edgeclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func projectAttestationFingerprintPlatform(path string, roots *WorkspaceRoots) (string, error) {
	validated, err := ValidateRegisteredWorkspace(path)
	if err != nil {
		return "", err
	}
	workspaceInfo, err := os.Lstat(validated)
	if err != nil || !workspaceInfo.IsDir() || workspaceInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("workspace attestation is unavailable")
	}
	gitPath := filepath.Join(validated, ".git")
	gitInfo, err := os.Lstat(gitPath)
	if err != nil || gitInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("git attestation is unavailable")
	}
	gitIdentity, err := projectGitAttestationIdentity(validated, gitPath, gitInfo, roots)
	if err != nil {
		return "", err
	}
	return hashProjectAttestation("project-attestation-v1", "workspace="+fileAttestationIdentity(workspaceInfo), "git="+gitIdentity), nil
}

func projectGitAttestationIdentity(workspacePath, gitPath string, gitInfo os.FileInfo, roots *WorkspaceRoots) (string, error) {
	identity := fileAttestationIdentity(gitInfo)
	commonPath := gitPath
	if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
		return "", err
	}
	if gitInfo.Mode().IsRegular() {
		content, readErr := os.ReadFile(gitPath)
		if readErr != nil || len(content) == 0 || len(content) > 4<<10 {
			return "", errors.New("git worktree attestation is unavailable")
		}
		line := strings.TrimSpace(string(content))
		if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
			return "", errors.New("git worktree attestation is invalid")
		}
		commonPath = strings.TrimSpace(line[len("gitdir:"):])
		if !filepath.IsAbs(commonPath) {
			commonPath = filepath.Join(workspacePath, commonPath)
		}
		commonPath = filepath.Clean(commonPath)
		commonInfo, commonErr := os.Lstat(commonPath)
		if commonErr != nil || !commonInfo.IsDir() || commonInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("git worktree attestation is unavailable")
		}
		if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
			return "", err
		}
		identity += "|gitdir=" + commonPath + "|gitdir_identity=" + fileAttestationIdentity(commonInfo)
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
		return "", errors.New("git common directory attestation is unavailable")
	}
	if err := validateManagedGitMetadataPath(commonPath, roots); err != nil {
		return "", err
	}
	return identity + "|common=" + commonPath + "|common_identity=" + fileAttestationIdentity(commonInfo), nil
}

func validateManagedGitMetadataPath(path string, roots *WorkspaceRoots) error {
	if roots == nil {
		return nil
	}
	normalized, err := normalizeWorkspaceRoots(*roots)
	if err != nil {
		return errors.New("git metadata roots are unavailable")
	}
	path = filepath.Clean(path)
	for _, root := range []string{normalized.Dev, normalized.HTBLinux} {
		if path != root && pathInside(root, path) {
			if err := rejectSymlinkPath(path); err != nil {
				return errors.New("git metadata path contains a symlink")
			}
			return nil
		}
	}
	return errors.New("git metadata is outside authorized roots")
}

func fileAttestationIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("mode=%o|size=%d|mtime=%d", info.Mode().Perm(), info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("dev=%d|ino=%d|uid=%d|mode=%o", uint64(stat.Dev), uint64(stat.Ino), uint64(stat.Uid), info.Mode().Perm())
}
