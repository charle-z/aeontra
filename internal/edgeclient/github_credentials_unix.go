//go:build !windows

package edgeclient

import (
	"os"
	"syscall"
)

func ownedByCurrentUIDPortable(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}
