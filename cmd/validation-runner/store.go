package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const storeMountPath = "/pnpm-store"

func parseNumericUser(value string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("runner user must use numeric uid:gid format")
	}
	uid, err := strconv.Atoi(parts[0])
	if err != nil || uid < 0 {
		return 0, 0, fmt.Errorf("runner uid must be a non-negative integer")
	}
	gid, err := strconv.Atoi(parts[1])
	if err != nil || gid < 0 {
		return 0, 0, fmt.Errorf("runner gid must be a non-negative integer")
	}
	return uid, gid, nil
}

func prepareStore(path, user string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("store path must be a clean absolute path")
	}
	uid, gid, err := parseNumericUser(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o775); err != nil {
		return fmt.Errorf("creating runner store: %w", err)
	}
	if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return os.Lchown(current, uid, gid)
		}
		return os.Chown(current, uid, gid)
	}); err != nil {
		return fmt.Errorf("preparing runner store ownership: %w", err)
	}
	return nil
}
