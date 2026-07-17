package mcpserver

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/console"
)

const (
	consoleStorageLimitBytes int64 = 256 << 20
	maxConsoleStorageFiles         = 4096
)

func readConsoleStorageBudget(stateRoot, auditPath string) console.StorageData {
	budget := console.StorageData{LimitBytes: consoleStorageLimitBytes, State: "unavailable"}
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	if stateRoot == "." || stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return budget
	}
	rootInfo, err := os.Lstat(stateRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return budget
	}
	seen := make(map[string]struct{})
	count := 0
	walkErr := filepath.WalkDir(stateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("storage entry unavailable")
		}
		if path == stateRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("storage symlink rejected")
		}
		count++
		if count > maxConsoleStorageFiles {
			return errors.New("storage entry limit exceeded")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("storage file unavailable")
		}
		absolute := filepath.Clean(path)
		seen[absolute] = struct{}{}
		classifyConsoleStorage(&budget, absolute, info.Size())
		return nil
	})
	if walkErr != nil {
		return console.StorageData{LimitBytes: consoleStorageLimitBytes, State: "unavailable"}
	}
	auditPath = filepath.Clean(strings.TrimSpace(auditPath))
	if auditPath != "." && auditPath != "" && filepath.IsAbs(auditPath) {
		if _, exists := seen[auditPath]; !exists {
			if info, err := os.Lstat(auditPath); err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() {
				classifyConsoleStorage(&budget, auditPath, info.Size())
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return console.StorageData{LimitBytes: consoleStorageLimitBytes, State: "unavailable"}
			}
		}
	}
	budget.TotalBytes = budget.DatabaseBytes + budget.WALBytes + budget.LogBytes
	budget.Available = true
	budget.State = "healthy"
	if budget.TotalBytes >= budget.LimitBytes*9/10 {
		budget.State = "degraded"
	} else if budget.TotalBytes >= budget.LimitBytes*3/4 {
		budget.State = "nearing_limit"
	}
	return budget
}

func classifyConsoleStorage(budget *console.StorageData, path string, size int64) {
	if budget == nil || size < 0 {
		return
	}
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(name, "-wal"), strings.HasSuffix(name, "-shm"):
		budget.WALBytes += size
	case strings.HasSuffix(name, ".db"), strings.HasSuffix(name, ".sqlite"), strings.HasSuffix(name, ".sqlite3"):
		budget.DatabaseBytes += size
	case strings.HasSuffix(name, ".log"), strings.HasSuffix(name, ".jsonl"):
		budget.LogBytes += size
	}
}
