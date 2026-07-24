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
	return renameNoReplaceWith(source, destination, func(source, destination string) error {
		return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
	})
}

func renameNoReplaceWith(source, destination string, atomicRename func(string, string) error) error {
	if atomicRename == nil {
		return errors.New("atomic no-replace rename is unavailable")
	}
	err := atomicRename(source, destination)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	sourceInfo, statErr := os.Lstat(source)
	if statErr != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return err
	}
	return portableDirectoryRenameNoReplace(source, destination)
}

func portableDirectoryRenameNoReplace(source, destination string) error {
	return portableDirectoryRenameNoReplaceWith(source, destination, func(source, destination string) error {
		return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE)
	})
}

func portableDirectoryRenameNoReplaceWith(source, destination string, exchange func(string, string) error) error {
	if _, err := os.Lstat(destination); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return err
	}
	placeholder, err := os.Lstat(destination)
	if err != nil || !placeholder.IsDir() || placeholder.Mode().Perm()&0o077 != 0 {
		removeRenamePlaceholder(destination, placeholder)
		if err != nil {
			return err
		}
		return errors.New("rename placeholder is unsafe")
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 0 {
		removeRenamePlaceholder(destination, placeholder)
		if err != nil {
			return err
		}
		return errors.New("rename placeholder is not empty")
	}
	if exchange == nil {
		removeRenamePlaceholder(destination, placeholder)
		return errors.New("atomic rename exchange is unavailable")
	}
	if err := exchange(source, destination); err != nil {
		removeRenamePlaceholder(destination, placeholder)
		return err
	}
	movedPlaceholder, err := os.Lstat(source)
	if err != nil || !os.SameFile(placeholder, movedPlaceholder) || !movedPlaceholder.IsDir() {
		return errors.New("rename exchange did not preserve the verified placeholder")
	}
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) != 0 {
		return errors.New("rename exchange placeholder is not empty")
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	return nil
}

func removeRenamePlaceholder(path string, expected os.FileInfo) {
	if expected == nil {
		return
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(expected, current) || !current.IsDir() {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return
	}
	_ = os.Remove(path)
}

func migrationSystemErrorCategory(err error) string {
	switch {
	case errors.Is(err, unix.EXDEV):
		return "cross_device"
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return "permission"
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL), errors.Is(err, unix.EOPNOTSUPP):
		return "unsupported"
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY):
		return "destination_conflict"
	case errors.Is(err, unix.ENOENT):
		return "path_missing"
	case errors.Is(err, unix.EBUSY):
		return "busy"
	default:
		return ""
	}
}
