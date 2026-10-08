//go:build linux

package edgeclient

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openOwnedRegularBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(rootInfo) {
		return nil, nil, errStableFileUnsafe
	}
	rootFile, err := os.Open(root)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil, os.ErrNotExist
		}
		return nil, nil, errStableFileUnsafe
	}
	defer rootFile.Close()
	openedRoot, err := rootFile.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return nil, nil, errStableFileUnsafe
	}
	fd, err := unix.Openat2(int(rootFile.Fd()), relative, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	file := os.NewFile(uintptr(fd), relative)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, errStableFileUnsafe
	}
	return file, info, nil
}

func openOwnedDirectoryBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(rootInfo) {
		return nil, nil, errStableFileUnsafe
	}
	rootFile, err := os.Open(root)
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	defer rootFile.Close()
	openedRoot, err := rootFile.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return nil, nil, errStableFileUnsafe
	}
	fd, err := unix.Openat2(int(rootFile.Fd()), relative, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, nil, os.ErrNotExist
		}
		return nil, nil, errStableFileUnsafe
	}
	directory := os.NewFile(uintptr(fd), relative)
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || !ownedByCurrentUIDPortable(info) {
		_ = directory.Close()
		return nil, nil, errStableFileUnsafe
	}
	return directory, info, nil
}
