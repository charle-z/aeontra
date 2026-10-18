//go:build !windows

package buildspike

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	BuilderUser      string
	BuilderUID       int
	RuntimeRoot      string
	StateRoot        string
	CacheRoot        string
	BuildkitdPath    string
	BuildctlPath     string
	RootlesskitPath  string
	CPUQuotaPercent  int
	MemoryHighMiB    int
	MemoryMaxMiB     int
	PIDsMax          int
	IOWeight         int
	MaxOutputBytes   int64
	MaxArtifactBytes int64
}

func DefaultConfig(uid int) Config {
	return Config{
		BuilderUser:      "mcp-build",
		BuilderUID:       uid,
		RuntimeRoot:      "/run/mcp-devbox-buildkit",
		StateRoot:        "/var/lib/mcp-devbox-buildkit",
		CacheRoot:        "/var/cache/mcp-devbox-buildkit",
		BuildkitdPath:    "/usr/local/lib/mcp-devbox-builder/buildkitd",
		BuildctlPath:     "/usr/local/lib/mcp-devbox-builder/buildctl",
		RootlesskitPath:  "/usr/bin/rootlesskit",
		CPUQuotaPercent:  65,
		MemoryHighMiB:    1280,
		MemoryMaxMiB:     1792,
		PIDsMax:          512,
		IOWeight:         25,
		MaxOutputBytes:   1 << 20,
		MaxArtifactBytes: 2 << 30,
	}
}

func (c Config) Validate() error {
	if c.BuilderUser != "mcp-build" || c.BuilderUID <= 0 || c.BuilderUID > 1<<30 {
		return errors.New("buildspike: dedicated non-root builder identity is invalid")
	}
	if c.RuntimeRoot != "/run/mcp-devbox-buildkit" || c.StateRoot != "/var/lib/mcp-devbox-buildkit" || c.CacheRoot != "/var/cache/mcp-devbox-buildkit" {
		return errors.New("buildspike: builder roots are invalid")
	}
	if c.BuildkitdPath != "/usr/local/lib/mcp-devbox-builder/buildkitd" || c.BuildctlPath != "/usr/local/lib/mcp-devbox-builder/buildctl" || c.RootlesskitPath != "/usr/bin/rootlesskit" {
		return errors.New("buildspike: builder executable path is invalid")
	}
	if c.CPUQuotaPercent != 50 && c.CPUQuotaPercent != 65 && c.CPUQuotaPercent != 80 {
		return errors.New("buildspike: CPU quota is not a calibrated candidate")
	}
	if c.MemoryHighMiB < 256 || c.MemoryMaxMiB < c.MemoryHighMiB || c.MemoryMaxMiB > 4096 || c.PIDsMax < 32 || c.PIDsMax > 4096 || c.IOWeight < 1 || c.IOWeight > 1000 || c.MaxOutputBytes < 4096 || c.MaxOutputBytes > 16<<20 || c.MaxArtifactBytes < 1<<20 || c.MaxArtifactBytes > 8<<30 {
		return errors.New("buildspike: resource bounds are invalid")
	}
	for _, path := range []string{c.RuntimeRoot, c.StateRoot, c.CacheRoot, c.BuildkitdPath, c.BuildctlPath, c.RootlesskitPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("buildspike: path is invalid")
		}
	}
	return nil
}

func (c Config) validateForPlan() error {
	if c.BuilderUser != "mcp-build" || c.BuilderUID <= 0 || c.BuilderUID > 1<<30 {
		return errors.New("buildspike: dedicated non-root builder identity is invalid")
	}
	if c.BuildkitdPath != "/usr/local/lib/mcp-devbox-builder/buildkitd" || c.BuildctlPath != "/usr/local/lib/mcp-devbox-builder/buildctl" || c.RootlesskitPath != "/usr/bin/rootlesskit" {
		return errors.New("buildspike: builder executable path is invalid")
	}
	if c.CPUQuotaPercent != 50 && c.CPUQuotaPercent != 65 && c.CPUQuotaPercent != 80 {
		return errors.New("buildspike: CPU quota is not a calibrated candidate")
	}
	if c.MemoryHighMiB < 256 || c.MemoryMaxMiB < c.MemoryHighMiB || c.MemoryMaxMiB > 4096 || c.PIDsMax < 32 || c.PIDsMax > 4096 || c.IOWeight < 1 || c.IOWeight > 1000 || c.MaxOutputBytes < 4096 || c.MaxOutputBytes > 16<<20 || c.MaxArtifactBytes < 1<<20 || c.MaxArtifactBytes > 8<<30 {
		return errors.New("buildspike: resource bounds are invalid")
	}
	for _, item := range []string{c.RuntimeRoot, c.StateRoot, c.CacheRoot} {
		if !filepath.IsAbs(item) || filepath.Clean(item) != item || item == "/" || item == "/run/buildkit" || item == "/var/lib/buildkit" || item == "/var/cache/buildkit" {
			return errors.New("buildspike: builder root is invalid")
		}
		info, statErr := os.Lstat(item)
		if statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0) {
			return errors.New("buildspike: builder root is unsafe")
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return errors.New("buildspike: builder root is unavailable")
		}
	}
	return nil
}

func (c Config) SystemdProperties() (map[string]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return map[string]string{
		"CPUQuota":        fmt.Sprintf("%d%%", c.CPUQuotaPercent),
		"MemoryHigh":      fmt.Sprintf("%dM", c.MemoryHighMiB),
		"MemoryMax":       fmt.Sprintf("%dM", c.MemoryMaxMiB),
		"TasksMax":        fmt.Sprintf("%d", c.PIDsMax),
		"IOWeight":        fmt.Sprintf("%d", c.IOWeight),
		"KillMode":        "control-group",
		"Delegate":        "yes",
		"User":            c.BuilderUser,
		"NoNewPrivileges": "no",
	}, nil
}
