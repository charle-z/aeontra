//go:build !windows

package edgelifecycle

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func currentEUID() int { return os.Geteuid() }

func currentEGID() int { return os.Getegid() }

func requireOwnedPath(path string, expectedUID, expectedGID int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("path ownership is unavailable")
	}
	if int(stat.Uid) != expectedUID || int(stat.Gid) != expectedGID {
		return errors.New("path owner does not match the Edge identity")
	}
	return nil
}

func renameNoReplace(source, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
