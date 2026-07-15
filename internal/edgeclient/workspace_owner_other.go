//go:build !linux

package edgeclient

import (
	"errors"
	"os"
)

func requireCurrentOwner(os.FileInfo) error {
	return errors.New("workspace registry requires Linux or WSL2")
}
