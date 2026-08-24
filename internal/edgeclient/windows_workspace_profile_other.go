//go:build !windows

package edgeclient

import "errors"

func defaultWindowsWorkspaceRoot(string) string { return "" }

func ConfigureWindowsWorkspaceRoot(string) error {
	return errors.New("windows workcell requires a native Windows Edge")
}

func validateWindowsWorkcellPath(string, string) (string, error) {
	return "", errors.New("windows workcell requires a native Windows Edge")
}
