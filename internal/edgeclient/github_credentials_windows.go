//go:build windows

package edgeclient

import "os"

func ownedByCurrentUIDPortable(os.FileInfo) bool { return true }
