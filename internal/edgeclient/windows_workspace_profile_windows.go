//go:build windows

package edgeclient

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

var windowsWorkspaceRootConfig struct {
	sync.Mutex
	path string
}

func defaultWindowsWorkspaceRoot(home string) string {
	windowsWorkspaceRootConfig.Lock()
	configured := windowsWorkspaceRootConfig.path
	windowsWorkspaceRootConfig.Unlock()
	if configured != "" {
		return configured
	}
	return filepath.Join(home, "workspaces")
}

// ConfigureWindowsWorkspaceRoot binds this process to one administrator-owned
// local root before it opens registries or accepts operations. A second,
// different root is rejected so a request cannot retarget a live Edge.
func ConfigureWindowsWorkspaceRoot(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if !IsWindowsLocalPath(root) {
		return errors.New("windows workspace root is unsafe")
	}
	handle, err := OpenWindowsWorkcell(root, root)
	if err != nil {
		return err
	}
	root = handle.FinalPath()
	_ = handle.Close()
	windowsWorkspaceRootConfig.Lock()
	defer windowsWorkspaceRootConfig.Unlock()
	if windowsWorkspaceRootConfig.path != "" && !strings.EqualFold(windowsWorkspaceRootConfig.path, root) {
		return errors.New("windows workspace root is already configured")
	}
	windowsWorkspaceRootConfig.path = root
	return nil
}

func validateWindowsWorkcellPath(candidate, root string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "" || candidate == "" || strings.EqualFold(root, candidate) {
		return "", errors.New("windows workcell path must be below its registered root")
	}
	workspace, err := OpenWindowsWorkcell(root, candidate)
	if err != nil {
		return "", err
	}
	defer workspace.Close()
	return workspace.FinalPath(), nil
}
