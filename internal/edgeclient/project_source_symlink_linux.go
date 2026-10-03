//go:build linux

package edgeclient

import (
	"os"

	"golang.org/x/sys/unix"
)

// O_PATH pins the link itself. readlinkat's empty path reads that descriptor,
// not a subsequently exchanged directory entry or any link target.
func openPinnedProjectSourceSymlink(parentFD int, name string, before *unix.Stat_t) (*os.File, error) {
	fd, err := unix.Openat(parentFD, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil || after.Mode&unix.S_IFMT != unix.S_IFLNK || before.Dev != after.Dev || before.Ino != after.Ino {
		_ = unix.Close(fd)
		return nil, ErrProjectWorktreeUnsafe
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, ErrProjectWorktreeUnavailable
	}
	return file, nil
}

func readPinnedProjectSourceSymlink(file *os.File) ([]byte, error) {
	buffer := make([]byte, maxProjectWorktreeContentPathBytes+1)
	n, err := unix.Readlinkat(int(file.Fd()), "", buffer)
	if err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	if n == 0 || n >= len(buffer) {
		return nil, ErrProjectWorktreeUnsafe
	}
	return buffer[:n], nil
}
