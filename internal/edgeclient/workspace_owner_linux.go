//go:build linux

package edgeclient

import (
	"errors"
	"os"
	"syscall"
)

func requireCurrentOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("workspace path is not owned by the Edge user")
	}
	return nil
}
