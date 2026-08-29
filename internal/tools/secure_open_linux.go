//go:build linux

package tools

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openRegularBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("unsafe repository root")
	}
	rootFile, err := os.Open(root)
	if err != nil {
		return nil, nil, err
	}
	defer rootFile.Close()
	openedRoot, err := rootFile.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return nil, nil, fmt.Errorf("repository root identity changed")
	}
	fd, err := unix.Openat2(int(rootFile.Fd()), relative, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), relative)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("unsafe regular file")
	}
	return file, info, nil
}
