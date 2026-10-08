package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// openContainedRegular resolves every component relative to an already-opened jail
// root. Linux rejects all symlink components in the kernel; other supported hosts use
// os.Root to prevent an intermediate rename or reparse point from escaping the root.
func openContainedRegular(root, path string) (*os.File, os.FileInfo, error) {
	cleanRoot, cleanPath := filepath.Clean(root), filepath.Clean(path)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("unsafe regular file")
	}
	return openRegularBeneath(cleanRoot, relative)
}
