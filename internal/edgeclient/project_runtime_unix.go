//go:build !windows

package edgeclient

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	projectRuntimeStateDirectory    = "project-runtime"
	projectRuntimeCacheDirectory    = "project-cache"
	projectRuntimeArtifactDirectory = "project-artifacts"
)

// projectRuntimeRootPaths derives roots from the Edge state root and registry
// workspace ID without touching the filesystem. Workspace IDs are validated
// before they become path components.
func projectRuntimeRootPaths(stateRoot string, workspace Workspace) (ProjectRuntimeRoots, []string, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if !filepath.IsAbs(stateRoot) || stateRoot == string(filepath.Separator) || stateRoot == "." || !workspaceIDPattern.MatchString(workspace.ID) {
		return ProjectRuntimeRoots{}, nil, errors.New("project runtime root is invalid")
	}
	workspacePath := filepath.Clean(workspace.Path)
	if filepath.IsAbs(workspacePath) && (workspacePath == stateRoot || pathInside(workspacePath, stateRoot)) {
		return ProjectRuntimeRoots{}, nil, errors.New("project runtime root overlaps source")
	}
	roots := ProjectRuntimeRoots{
		Runtime:   filepath.Join(stateRoot, projectRuntimeStateDirectory, workspace.ID),
		Cache:     filepath.Join(stateRoot, projectRuntimeCacheDirectory, workspace.ID),
		Artifacts: filepath.Join(stateRoot, projectRuntimeArtifactDirectory, workspace.ID),
	}
	directories := []string{
		stateRoot,
		filepath.Dir(roots.Runtime), filepath.Dir(roots.Cache), filepath.Dir(roots.Artifacts),
		roots.Runtime, roots.Cache, roots.Artifacts, projectRuntimeControlRoot(roots),
		filepath.Join(roots.Runtime, "home"),
	}
	for _, root := range directories[1:] {
		if !pathInside(stateRoot, root) || root == stateRoot {
			return ProjectRuntimeRoots{}, nil, errors.New("project runtime root escaped state")
		}
	}
	return roots, directories, nil
}

// prepareProjectRuntimeRoots creates owner-only roots derived from the Edge
// state root and registry workspace ID. Every ancestor is checked for
// symlinks by preparePrivateRoot.
func prepareProjectRuntimeRoots(stateRoot string, workspace Workspace) (ProjectRuntimeRoots, error) {
	roots, directories, err := projectRuntimeRootPaths(stateRoot, workspace)
	if err != nil {
		return ProjectRuntimeRoots{}, err
	}
	for i, root := range directories {
		if err := preparePrivateRoot(root); err != nil {
			if i == 0 {
				return ProjectRuntimeRoots{}, errors.New("project runtime state is unavailable")
			}
			return ProjectRuntimeRoots{}, errors.New("project runtime root is unavailable")
		}
	}
	return roots, nil
}

// validateProjectRuntimeRoots verifies the same derived roots without creating
// directories or changing permissions. Read-only status must not repair them.
func validateProjectRuntimeRoots(stateRoot string, workspace Workspace) (ProjectRuntimeRoots, error) {
	roots, directories, err := projectRuntimeRootPaths(stateRoot, workspace)
	if err != nil {
		return ProjectRuntimeRoots{}, err
	}
	for i, root := range directories {
		if err := validatePrivateRoot(root); err != nil {
			if i == 0 {
				return ProjectRuntimeRoots{}, errors.New("project runtime state is unavailable")
			}
			return ProjectRuntimeRoots{}, errors.New("project runtime root is unavailable")
		}
	}
	return roots, nil
}

// defaultProjectRuntimeStateRoot is only a compatibility path for callers
// that construct the low-level direct-workcell request themselves. Production
// project operations always provide the configured Edge state root.
func defaultProjectRuntimeStateRoot() string {
	return filepath.Join(os.TempDir(), "mcp-devbox-runtime-state")
}

func projectRuntimeWorkspaceFingerprint(workspacePath string) (string, error) {
	workspacePath = filepath.Clean(workspacePath)
	if !filepath.IsAbs(workspacePath) {
		return "", errors.New("workspace path is not absolute")
	}
	info, err := os.Lstat(workspacePath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
		return "", errors.New("workspace identity is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return "", errors.New("workspace identity is unavailable")
	}
	// Device + inode identify the directory object without recording mutable
	// contents. A replacement directory at the same path therefore fails
	// attestation even when it has identical Git contents.
	return fmt.Sprintf("dev:%d:ino:%d", stat.Dev, stat.Ino), nil
}

func projectRuntimeFingerprint(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func projectRuntimeMountFingerprint(workspacePath, workspaceFingerprint string, roots ProjectRuntimeRoots, version string) string {
	return projectRuntimeFingerprint(version, filepath.Clean(workspacePath), workspaceFingerprint,
		filepath.Clean(roots.Runtime), filepath.Clean(roots.Cache), filepath.Clean(roots.Artifacts))
}

func projectRuntimeEndpointFingerprint(endpoint *RootlessContainerEndpoint) string {
	if endpoint == nil {
		return ""
	}
	return projectRuntimeFingerprint(endpoint.Engine, filepath.Clean(endpoint.SocketPath), filepath.Clean(endpoint.Executable))
}
