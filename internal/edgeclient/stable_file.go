package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errStableFileUnsafe = errors.New("file identity is unsafe")

func openStableOwnedRegular(path string) (*os.File, os.FileInfo, error) {
	return openStableOwnedRegularUnder(filepath.Dir(filepath.Clean(path)), path)
}

// openStableOwnedRegularUnder opens a file descriptor relative to the managed root.
// The platform implementation prevents a concurrent intermediate symlink/reparse
// replacement from escaping that root.
func openStableOwnedRegularUnder(root, path string) (*os.File, os.FileInfo, error) {
	cleanRoot, cleanPath := filepath.Clean(root), filepath.Clean(path)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, errStableFileUnsafe
	}
	file, info, err := openOwnedRegularBeneath(cleanRoot, relative)
	if err != nil {
		return nil, nil, err
	}
	if !ownedByCurrentUIDPortable(info) {
		_ = file.Close()
		return nil, nil, errStableFileUnsafe
	}
	return file, info, nil
}

func openStableOwnedDirectoryUnder(root, path string) (*os.File, os.FileInfo, error) {
	cleanRoot, cleanPath := filepath.Clean(root), filepath.Clean(path)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, errStableFileUnsafe
	}
	return openOwnedDirectoryBeneath(cleanRoot, relative)
}
