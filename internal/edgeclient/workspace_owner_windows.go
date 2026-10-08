//go:build windows

package edgeclient

import (
	"errors"
	"os"
)

func requireCurrentOwner(os.FileInfo) error {
	return errors.New("POSIX workspace ownership is unavailable on Windows")
}

func validateRegisteredWorkspace(string) (string, error) {
	return "", errors.New("generic sandbox workspaces are unavailable on Windows")
}
