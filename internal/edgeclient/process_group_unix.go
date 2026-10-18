//go:build !windows

package edgeclient

import "syscall"

func processGroupAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
