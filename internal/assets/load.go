package assets

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

// LoadLibrary reads one owner-private regular manifest outside all repository
// roots. The immutable snapshot is never writable or reloadable through MCP.
func LoadLibrary(path string, repositoryRoots []string) (*Library, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || policy.IsSecretPath(path) {
		return nil, errors.New("asset library path is invalid")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return nil, errors.New("asset library must be a direct regular file")
	}
	for _, root := range repositoryRoots {
		if realRoot, err := filepath.EvalSymlinks(root); err == nil {
			root = realRoot
		}
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return nil, errors.New("asset library must be outside repository roots")
		}
	}
	parentInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parentInfo.IsDir() || !privateManifestOwner(parentInfo) {
		return nil, errors.New("asset library parent requires owner-private Linux permissions")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 1 || before.Size() > MaxLibraryBytes {
		return nil, errors.New("asset library file is unsafe or exceeds bounds")
	}
	if !privateManifestOwner(before) {
		return nil, errors.New("asset library requires owner-private Linux permissions")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("asset library unavailable")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || !privateManifestOwner(after) {
		return nil, errors.New("asset library identity changed during open")
	}
	body, err := io.ReadAll(io.LimitReader(file, MaxLibraryBytes+1))
	if err != nil {
		return nil, errors.New("asset library read unavailable")
	}
	return ParseLibrary(body)
}
