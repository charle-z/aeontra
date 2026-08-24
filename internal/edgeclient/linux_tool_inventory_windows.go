//go:build windows

package edgeclient

import (
	"errors"
	"os"
)

func ValidateLinuxToolInventory([]LinuxToolInventoryEntry) error {
	return errors.New("Linux tool inventory requires a Linux Edge")
}

func atomicWorkspaceFile(string, []byte, os.FileMode) error {
	return errors.New("Linux workspace authorization requires a Linux Edge")
}
