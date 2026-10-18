//go:build !windows

package buildspike

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func ValidateBuildKitSocket(socketPath, runtimeRoot string, expectedUID int) error {
	if expectedUID <= 0 || !filepath.IsAbs(socketPath) || !filepath.IsAbs(runtimeRoot) || filepath.Clean(socketPath) != socketPath || filepath.Clean(runtimeRoot) != runtimeRoot {
		return errors.New("buildspike: socket request is invalid")
	}
	for _, forbidden := range []string{"/var/run/docker.sock", "/run/docker.sock", "/run/buildkit/buildkitd.sock"} {
		if socketPath == forbidden {
			return errors.New("buildspike: rootful endpoint rejected")
		}
	}
	relative, err := filepath.Rel(runtimeRoot, socketPath)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("buildspike: socket escapes runtime root")
	}
	current := socketPath
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("buildspike: socket path unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("buildspike: symlinked endpoint rejected")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != expectedUID || info.Mode().Perm()&0o077 != 0 {
			return errors.New("buildspike: socket ownership or mode is unsafe")
		}
		if current == socketPath && info.Mode()&os.ModeSocket == 0 {
			return errors.New("buildspike: endpoint is not a socket")
		}
		if current == runtimeRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return errors.New("buildspike: runtime root unavailable")
		}
		current = parent
	}
	return nil
}
