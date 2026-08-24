//go:build !windows

package sandboxexecutor

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func canonicalDirectory(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", errors.New("path is not absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	// Workspace and state roots are administrator-controlled absolute mounts,
	// canonicalized before use and never accepted from an execution request.
	// codeql[go/path-injection]
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return resolved, nil
}

func validatePrivateStateRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private state root is unavailable")
	}
	if info.Mode().Perm() != 0o700 {
		return errors.New("private state root must have mode 0700")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() {
		return errors.New("private state root must be owned by the executor identity")
	}
	receipts, err := os.Lstat(filepath.Join(root, "receipts"))
	if err != nil || !receipts.IsDir() || receipts.Mode()&os.ModeSymlink != 0 || receipts.Mode().Perm() != 0o700 {
		return errors.New("receipt root must be an owner-only directory")
	}
	return nil
}
