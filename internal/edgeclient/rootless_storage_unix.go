//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var rootlessStorageDriverPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// RootlessContainerStorageDriver returns only the bounded storage-driver name
// reported by the already validated user-owned rootless engine. Socket paths,
// engine arguments and command output remain private to the Edge.
func RootlessContainerStorageDriver(ctx context.Context, endpoint *RootlessContainerEndpoint, toolPath string) (string, error) {
	return rootlessContainerStorageDriver(ctx, endpoint, toolPath, execContainerCommandRunner{}, rootlessContainerClientEnvironment)
}

func rootlessContainerStorageDriver(ctx context.Context, endpoint *RootlessContainerEndpoint, toolPath string, runner ContainerCommandRunner, environmentBuilder rootlessContainerEnvironmentBuilder) (string, error) {
	if endpoint == nil || runner == nil || environmentBuilder == nil || !filepath.IsAbs(endpoint.Executable) ||
		(endpoint.Engine != "podman" && endpoint.Engine != "docker") {
		return "", errors.New("rootless storage driver inspection is unavailable")
	}
	if strings.TrimSpace(toolPath) == "" {
		toolPath = openCodeDefaultToolPath
	}
	environment, err := environmentBuilder(endpoint, toolPath)
	if err != nil {
		return "", errors.New("rootless storage driver environment is unavailable")
	}
	args := rootlessEnginePrefix(endpoint)
	switch endpoint.Engine {
	case "podman":
		args = append(args, "info", "--format", "{{.Store.GraphDriverName}}")
	case "docker":
		args = append(args, "info", "--format", "{{.Driver}}")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := runner.Run(commandCtx, endpoint.Executable, args, environment)
	if err != nil {
		return "", errors.New("rootless storage driver inspection failed")
	}
	driver := strings.ToLower(strings.TrimSpace(string(output)))
	if !rootlessStorageDriverPattern.MatchString(driver) {
		return "", errors.New("rootless storage driver response is invalid")
	}
	return driver, nil
}

// RootlessContainerStoragePosture groups the engine-reported driver into a
// small public posture. Unknown drivers remain observable without being
// optimistically classified as copy-on-write.
func RootlessContainerStoragePosture(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "overlay", "overlay2", "fuse-overlayfs", "btrfs", "zfs":
		return "copy_on_write"
	case "vfs":
		return "degraded"
	default:
		return "unknown"
	}
}
