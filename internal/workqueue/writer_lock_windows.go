//go:build windows

package workqueue

import (
	"errors"
	"os"
)

func acquireWriterLock(string, string) (*os.File, error) {
	return nil, errors.New("workqueue: single-writer lock is unsupported on this platform")
}

func releaseWriterLock(file *os.File) {
	if file != nil {
		_ = file.Close()
	}
}

func ownedByCurrentUser(os.FileInfo) bool { return true }

func syncDirectory(string) error { return nil }
