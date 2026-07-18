//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRootlessContainerClientEnvironmentIncludesOwnedRuntimeRoot(t *testing.T) {
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: filepath.Join(runtimeRoot, "podman", "podman.sock")}
	environment := rootlessContainerClientEnvironment(endpoint, openCodeDefaultToolPath)
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "PATH="+openCodeDefaultToolPath) || !strings.Contains(joined, "HOME=") || !strings.Contains(joined, "XDG_RUNTIME_DIR="+runtimeRoot) {
		t.Fatalf("environment=%q", environment)
	}
}

func TestRootlessContainerClientEnvironmentRejectsForeignRuntimeRoot(t *testing.T) {
	endpoint := &RootlessContainerEndpoint{Engine: "podman", SocketPath: "/run/user/999999/podman/podman.sock"}
	for _, value := range rootlessContainerClientEnvironment(endpoint, openCodeDefaultToolPath) {
		if strings.HasPrefix(value, "XDG_RUNTIME_DIR=") {
			t.Fatalf("foreign runtime root accepted: %q", value)
		}
	}
}
