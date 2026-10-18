//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
)

func safeLinuxWorkcellReadonlyDirectory(path, allowedRoot string, ownerUID int) (string, bool) {
	path = filepath.Clean(path)
	allowedRoot = filepath.Clean(allowedRoot)
	if ownerUID < 0 || !filepath.IsAbs(path) || !filepath.IsAbs(allowedRoot) || !pathInside(allowedRoot, path) || path == allowedRoot {
		return "", false
	}
	if err := rejectSymlinkPath(path); err != nil {
		return "", false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByUID(info, ownerUID) || info.Mode().Perm()&0o022 != 0 {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(resolved) != path || !pathInside(allowedRoot, resolved) || filepath.Clean(resolved) == allowedRoot {
		return "", false
	}
	return path, true
}
