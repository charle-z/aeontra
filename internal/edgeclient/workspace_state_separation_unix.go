//go:build !windows

package edgeclient

import (
	"errors"
	"path/filepath"
	"strings"
)

func validateWorkspaceStateSeparation(stateRoot string, roots WorkspaceRoots) error {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if !filepath.IsAbs(stateRoot) {
		return errors.New("edge state root is unsafe")
	}
	for _, root := range []string{roots.Dev, roots.HTBLinux} {
		if root != "" && (pathInside(root, stateRoot) || pathInside(stateRoot, root) || filepath.Clean(root) == stateRoot) {
			return errors.New("workspace roots must not overlap private Edge state")
		}
	}
	return nil
}
