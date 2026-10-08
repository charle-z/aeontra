package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func createDevGitTransportRoot(stateRoot, sourceDir string) (string, func(), error) {
	privateRoot := filepath.Join(filepath.Clean(stateRoot), "github-runtime")
	if err := preparePrivateRoot(privateRoot); err != nil {
		return "", nil, errors.New("development Git transport root is unsafe")
	}
	root, err := os.MkdirTemp(privateRoot, ".git-transport-*")
	if err != nil {
		return "", nil, errors.New("development Git transport root is unavailable")
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if err := preparePrivateRoot(root); err != nil {
		cleanup()
		return "", nil, errors.New("development Git transport root is unsafe")
	}
	if _, err := devGitObjectDirectory(sourceDir); err != nil {
		cleanup()
		return "", nil, err
	}
	return root, cleanup, nil
}

func configureDevGitAlternates(transportRoot, sourceDir string) error {
	objects, err := devGitObjectDirectory(sourceDir)
	if err != nil {
		return err
	}
	alternates := filepath.Join(transportRoot, "objects", "info", "alternates")
	file, err := os.OpenFile(alternates, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("development Git alternate object store is unavailable")
	}
	if err := securePrivateFile(file); err != nil {
		_ = file.Close()
		return errors.New("development Git alternate object store is unsafe")
	}
	_, writeErr := file.WriteString(objects + "\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("development Git alternate object store is unavailable")
	}
	return nil
}

func devGitObjectDirectory(sourceDir string) (string, error) {
	metadata := filepath.Join(filepath.Clean(sourceDir), ".git")
	info, err := os.Lstat(metadata)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("development Git metadata is unsafe")
	}
	commonDir := metadata
	if !info.IsDir() {
		if !info.Mode().IsRegular() || info.Size() < 8 || info.Size() > 4096 {
			return "", errors.New("development Git worktree metadata is unsafe")
		}
		body, err := os.ReadFile(metadata)
		if err != nil {
			return "", errors.New("development Git worktree metadata is unavailable")
		}
		value := strings.TrimSpace(string(body))
		if !strings.HasPrefix(value, "gitdir: ") || strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("development Git worktree metadata is invalid")
		}
		gitDir := strings.TrimSpace(strings.TrimPrefix(value, "gitdir: "))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(sourceDir, gitDir)
		}
		gitDir = filepath.Clean(gitDir)
		worktreesDir := filepath.Dir(gitDir)
		if filepath.Base(worktreesDir) != "worktrees" {
			return "", errors.New("development Git worktree relationship is invalid")
		}
		commonDir = filepath.Dir(worktreesDir)
		gitDirInfo, gitDirErr := os.Lstat(gitDir)
		commonInfo, commonErr := os.Lstat(commonDir)
		if gitDirErr != nil || commonErr != nil || !gitDirInfo.IsDir() || !commonInfo.IsDir() || gitDirInfo.Mode()&os.ModeSymlink != 0 || commonInfo.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("development Git worktree relationship is unsafe")
		}
	}
	objects := filepath.Join(commonDir, "objects")
	objectsInfo, err := os.Lstat(objects)
	if err != nil || !objectsInfo.IsDir() || objectsInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("development Git object database is unsafe")
	}
	return objects, nil
}
