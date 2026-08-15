//go:build !windows

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	codexSandboxGitCommon = "/mcp-devbox-git-common"
)

type codexWorktreeGitMetadata struct {
	GitDir        string
	CommonDir     string
	SandboxGitDir string
}

func resolveCodexWorktreeGitMetadata(workspace Workspace, roots WorkspaceRoots) (codexWorktreeGitMetadata, bool, error) {
	gitFile := filepath.Join(workspace.Path, ".git")
	info, err := os.Lstat(gitFile)
	if errors.Is(err, os.ErrNotExist) {
		return codexWorktreeGitMetadata{}, false, nil
	}
	if err != nil {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell Git metadata is unavailable")
	}
	if info.IsDir() {
		return codexWorktreeGitMetadata{}, false, nil
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() < 10 || info.Size() > 4096 || !ownedByCurrentUIDPortable(info) {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell Git pointer is unsafe")
	}
	body, err := os.ReadFile(gitFile)
	if err != nil || !strings.HasPrefix(string(body), "gitdir: ") {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell Git pointer is invalid")
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(string(body), "gitdir: "))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workspace.Path, gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	commonPointer := filepath.Join(gitDir, "commondir")
	commonInfo, err := os.Lstat(commonPointer)
	if err != nil || !commonInfo.Mode().IsRegular() || commonInfo.Mode()&os.ModeSymlink != 0 || commonInfo.Mode().Perm()&0o022 != 0 || commonInfo.Size() < 2 || commonInfo.Size() > 4096 || !ownedByCurrentUIDPortable(commonInfo) {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell common Git pointer is unsafe")
	}
	commonBody, err := os.ReadFile(commonPointer)
	if err != nil {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell common Git pointer is unavailable")
	}
	commonDir := strings.TrimSpace(string(commonBody))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitDir, commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	worktreeMetadataRoot := filepath.Join(commonDir, "worktrees")
	canonicalPath := filepath.Dir(commonDir)
	if filepath.Base(commonDir) != ".git" || !pathInside(roots.Dev, canonicalPath) || canonicalPath == filepath.Clean(roots.Dev) ||
		!pathInside(worktreeMetadataRoot, gitDir) || gitDir == filepath.Clean(worktreeMetadataRoot) || pathInside(workspace.Path, commonDir) ||
		rejectSymlinkPath(gitDir) != nil || rejectSymlinkPath(commonDir) != nil {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell linked Git metadata is outside the managed development root")
	}
	for _, directory := range []string{gitDir, commonDir} {
		directoryInfo, statErr := os.Lstat(directory)
		if statErr != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || directoryInfo.Mode().Perm()&0o022 != 0 || !ownedByCurrentUIDPortable(directoryInfo) {
			return codexWorktreeGitMetadata{}, false, errors.New("codex workcell linked Git metadata is unsafe")
		}
	}
	if filepath.Clean(filepath.Dir(gitDir)) != filepath.Clean(worktreeMetadataRoot) {
		return codexWorktreeGitMetadata{}, false, errors.New("codex workcell linked Git directory is invalid")
	}
	return codexWorktreeGitMetadata{
		GitDir:        gitDir,
		CommonDir:     commonDir,
		SandboxGitDir: filepath.ToSlash(filepath.Join(codexSandboxGitCommon, "worktrees", filepath.Base(gitDir))),
	}, true, nil
}
