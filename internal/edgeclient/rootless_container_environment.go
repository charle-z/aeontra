//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func rootlessContainerClientEnvironment(endpoint *RootlessContainerEndpoint, toolPath string) []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "/nonexistent"
	}
	environment := []string{"PATH=" + toolPath, "HOME=" + home, "LANG=C", "LC_ALL=C"}
	if endpoint == nil {
		return environment
	}
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	if pathInside(runtimeRoot, endpoint.SocketPath) {
		environment = append(environment, "XDG_RUNTIME_DIR="+runtimeRoot)
	}
	return environment
}
