//go:build !windows

package edgeclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type rootlessContainerEnvironmentBuilder func(*RootlessContainerEndpoint, string) ([]string, error)

func rootlessContainerClientEnvironment(endpoint *RootlessContainerEndpoint, toolPath string) ([]string, error) {
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	return rootlessContainerClientEnvironmentFor(endpoint, toolPath, runtimeRoot, os.Geteuid())
}

func rootlessContainerClientEnvironmentFor(endpoint *RootlessContainerEndpoint, toolPath, runtimeRoot string, uid int) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "/nonexistent"
	}
	environment := []string{"PATH=" + toolPath, "HOME=" + home, "LANG=C", "LC_ALL=C"}
	if endpoint == nil {
		return environment, nil
	}
	if endpoint.Engine != "docker" && endpoint.Engine != "podman" {
		return nil, errors.New("rootless container endpoint is invalid")
	}
	if uid < 1 || !filepath.IsAbs(runtimeRoot) || !filepath.IsAbs(endpoint.SocketPath) {
		return nil, errors.New("rootless container endpoint is not Unix")
	}
	if err := validateRootlessContainerSocket(endpoint.SocketPath, runtimeRoot, uid); err != nil {
		return nil, fmt.Errorf("rootless container socket validation failed: %w", err)
	}
	validatedSocket := filepath.Clean(endpoint.SocketPath)
	unixEndpoint := "unix://" + validatedSocket
	environment = append(environment,
		"XDG_RUNTIME_DIR="+filepath.Clean(runtimeRoot),
		"CONTAINER_HOST="+unixEndpoint,
		"DOCKER_HOST="+unixEndpoint,
	)
	return environment, nil
}
