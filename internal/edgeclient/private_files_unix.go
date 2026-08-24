//go:build !windows

package edgeclient

import (
	"errors"
	"os"
)

func securePrivateRoot(path string, _ bool) error {
	return os.Chmod(path, 0o700)
}

func validatePrivateRootPlatform(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("edge state root is unsafe")
	}
	return nil
}

func securePrivateFile(file *os.File) error {
	return file.Chmod(0o600)
}

func validatePrivateFilePlatform(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("edge private file is unsafe")
	}
	return nil
}
