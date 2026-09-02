package sandboxexecutor

import (
	"errors"
	"os"
	"path/filepath"
)

// The L3 executor is Linux-only. This implementation keeps the pure protocol and
// request-validation tests portable without pretending to provide a Windows L3
// filesystem boundary; native Windows execution uses the Edge workcell boundary.
func canonicalDirectory(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", errors.New("path is not absolute")
	}
	cleaned := filepath.Clean(raw)
	info, err := os.Lstat(cleaned)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("path is not a direct directory")
	}
	return cleaned, nil
}

func validatePrivateStateRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private state root is unavailable")
	}
	receipts, err := os.Lstat(filepath.Join(root, "receipts"))
	if err != nil || !receipts.IsDir() || receipts.Mode()&os.ModeSymlink != 0 {
		return errors.New("receipt root is unavailable")
	}
	readiness, err := os.Lstat(filepath.Join(root, "readiness"))
	if err != nil || !readiness.IsDir() || readiness.Mode()&os.ModeSymlink != 0 {
		return errors.New("readiness root is unavailable")
	}
	return nil
}

// The private executor is Linux-only. Native Windows builds keep the protocol
// portable but do not claim POSIX directory-entry durability.
func syncDirectory(string) error { return nil }
