package mcpserver

import (
	"github.com/charle-z/mcp-devbox/internal/console"
	"golang.org/x/sys/windows"
)

func readDisk(data *console.SystemData) bool {
	root, err := windows.UTF16PtrFromString(`C:\`)
	if err != nil {
		return false
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(root, &available, &total, &free); err != nil {
		return false
	}
	data.DiskTotalBytes = total
	data.DiskAvailableBytes = available
	return true
}
