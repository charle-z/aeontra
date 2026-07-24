//go:build !windows

package buildspike

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func cancelProcessGroup(leader int) error {
	if leader < 2 {
		return errors.New("buildspike: process group leader is invalid")
	}
	members := processGroupMembers(leader)
	for _, pid := range members {
		if pid != leader {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		remaining := false
		for _, pid := range members {
			if pid != leader && processExists(pid) {
				remaining = true
				break
			}
		}
		if !remaining {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	err := syscall.Kill(leader, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func processGroupMembers(group int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	members := make([]int, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 2 {
			continue
		}
		body, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		line := string(body)
		end := strings.LastIndex(line, ")")
		if end < 0 || end+2 >= len(line) {
			continue
		}
		fields := strings.Fields(line[end+2:])
		if len(fields) < 3 {
			continue
		}
		processGroup, err := strconv.Atoi(fields[2])
		if err == nil && processGroup == group {
			members = append(members, pid)
		}
	}
	return members
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
