package brain

import (
	"errors"
	"os"
)

func ensurePrivateRootDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return errors.New("brain: private directory is unavailable")
		}
		return ensurePrivateDirectory(path)
	}
	if err != nil {
		return errors.New("brain: private directory is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("brain: source path is not a private directory")
	}
	if info.Mode().Perm() == 0o700 {
		return nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return errors.New("brain: private directory is unavailable")
	}
	if len(entries) != 0 {
		return errors.New("brain: private directory permissions must be 0700")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return errors.New("brain: private directory permissions must be 0700")
	}
	return ensurePrivateDirectory(path)
}
