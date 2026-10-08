//go:build !linux && !windows

package edgeclient

import (
	"errors"
	"os"
)

func requireCurrentOwner(os.FileInfo) error {
	return errors.New("workspace registry requires Linux or WSL2")
}

func validateRegisteredWorkspace(string) (string, error) {
	return "", errors.New("workspace registry is unsupported on this platform")
}
