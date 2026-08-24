package edgeclient

import (
	"errors"
	"os"
	"path/filepath"
)

func prepareProjectRegistryFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return createErr
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(path)
			return closeErr
		}
		if secureErr := securePrivateRegularPath(path); secureErr != nil {
			_ = os.Remove(path)
			return secureErr
		}
		if syncErr := syncProjectRegistryDirectory(filepath.Dir(path)); syncErr != nil {
			_ = os.Remove(path)
			return syncErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) || requirePrivateRegularFile(path) != nil {
		return errors.New("project registry is unsafe")
	}
	return nil
}

func syncProjectRegistryDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
