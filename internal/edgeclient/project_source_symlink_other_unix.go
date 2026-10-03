//go:build !linux && !windows

package edgeclient

import (
	"os"

	"golang.org/x/sys/unix"
)

// Platforms without Linux's pinned-link read fail closed rather than use a
// path-based readlink that could observe an exchanged directory entry.
func openPinnedProjectSourceSymlink(int, string, *unix.Stat_t) (*os.File, error) {
	return nil, ErrProjectWorktreeUnsafe
}

func readPinnedProjectSourceSymlink(*os.File) ([]byte, error) {
	return nil, ErrProjectWorktreeUnsafe
}
