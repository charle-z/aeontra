//go:build !windows

package workqueue

import (
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func acquireWriterLock(path, controllerID string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUser(info) {
			return nil, errors.New("workqueue: writer lock path is unsafe")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("workqueue: writer lock unavailable")
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.New("workqueue: writer lock unavailable")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("workqueue: writer lock is already held")
		}
		return nil, errors.New("workqueue: writer lock unavailable")
	}
	if err := file.Truncate(0); err != nil {
		releaseWriterLock(file)
		return nil, errors.New("workqueue: writer lock persistence failed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		releaseWriterLock(file)
		return nil, errors.New("workqueue: writer lock persistence failed")
	}
	if _, err := io.WriteString(file, controllerID+"\n"); err != nil || file.Sync() != nil || file.Chmod(0o600) != nil {
		releaseWriterLock(file)
		return nil, errors.New("workqueue: writer lock persistence failed")
	}
	return file, nil
}

func releaseWriterLock(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
