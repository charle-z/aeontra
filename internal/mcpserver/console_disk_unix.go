//go:build !windows

package mcpserver

import (
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/console"
)

func readDisk(data *console.SystemData) bool {
	var stat syscall.Statfs_t
	if syscall.Statfs("/", &stat) != nil {
		return false
	}
	data.DiskTotalBytes = stat.Blocks * uint64(stat.Bsize)
	data.DiskAvailableBytes = stat.Bavail * uint64(stat.Bsize)
	return true
}
