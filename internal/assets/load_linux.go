//go:build linux

package assets

import (
	"os"
	"syscall"
)

func privateManifestOwner(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0077 == 0 && info.Mode().Perm()&0400 != 0
}
