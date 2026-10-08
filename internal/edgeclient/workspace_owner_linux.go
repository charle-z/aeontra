//go:build linux

package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func validateRegisteredWorkspace(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) || path == string(os.PathSeparator) || isWindowsMount(path) {
		return "", errors.New("workspace path is unsafe")
	}
	if err := rejectSymlinkPath(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("workspace path is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != path {
		return "", errors.New("workspace path is unsafe")
	}
	if err := requireCurrentOwner(info); err != nil {
		return "", err
	}
	return path, nil
}

func requireCurrentOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("workspace path is not owned by the Edge user")
	}
	return nil
}
