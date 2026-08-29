//go:build !linux

package tools

import (
	"fmt"
	"os"
)

func openRegularBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, err
	}
	defer rootHandle.Close()
	before, err := rootHandle.Lstat(relative)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("unsafe regular file")
	}
	file, err := rootHandle.Open(relative)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file identity changed during open")
	}
	return file, after, nil
}
