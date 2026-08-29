//go:build !linux

package edgeclient

import "os"

func openOwnedRegularBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	defer rootHandle.Close()
	before, err := rootHandle.Lstat(relative)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, os.ErrNotExist
		}
		return nil, nil, errStableFileUnsafe
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errStableFileUnsafe
	}
	file, err := rootHandle.Open(relative)
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = file.Close()
		return nil, nil, errStableFileUnsafe
	}
	return file, after, nil
}

func openOwnedDirectoryBeneath(root, relative string) (*os.File, os.FileInfo, error) {
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	defer rootHandle.Close()
	before, err := rootHandle.Lstat(relative)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, os.ErrNotExist
		}
		return nil, nil, errStableFileUnsafe
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(before) {
		return nil, nil, errStableFileUnsafe
	}
	directory, err := rootHandle.Open(relative)
	if err != nil {
		return nil, nil, errStableFileUnsafe
	}
	after, err := directory.Stat()
	if err != nil || !after.IsDir() || !os.SameFile(before, after) {
		_ = directory.Close()
		return nil, nil, errStableFileUnsafe
	}
	return directory, after, nil
}
