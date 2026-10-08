//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
)

func syncProjectRegistryCreation(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
