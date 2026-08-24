//go:build windows

package edgeclient

import (
	"errors"
	"path/filepath"
	"strings"
)

func validateWorkspaceStateSeparation(stateRoot string, roots WorkspaceRoots) error {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if !IsWindowsLocalPath(stateRoot) || !IsWindowsLocalPath(roots.WindowsDev) {
		return errors.New("Windows Edge roots are unsafe")
	}
	if WindowsPathContained(roots.WindowsDev, stateRoot) || WindowsPathContained(stateRoot, roots.WindowsDev) {
		return errors.New("Windows workspace root must not overlap private Edge state")
	}
	return nil
}
