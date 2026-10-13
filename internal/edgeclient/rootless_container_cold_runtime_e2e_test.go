//go:build linux

package edgeclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Run only in the documented disposable mount namespace with a cold, read-only
// host runtime and the existing rootless Podman socket. No containers are changed.
func TestRootlessContainerColdRuntimeE2E(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_TEST_COLD_CONTAINER_RUNTIME") != "1" {
		t.Skip("requires the cold runtime mount-namespace fixture")
	}
	if os.Geteuid() == 0 {
		t.Fatal("acceptance must run as the rootless engine owner")
	}
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(os.Geteuid()))
	if _, err := os.Stat(filepath.Join(runtimeRoot, "libpod")); !os.IsNotExist(err) {
		t.Fatalf("fixture runtime is not cold: %v", err)
	}
	endpoints, err := DiscoverRootlessContainerEndpoints(os.Geteuid(), openCodeDefaultToolPath)
	if err != nil {
		t.Fatal(err)
	}
	var endpoint *RootlessContainerEndpoint
	for _, candidate := range endpoints {
		if candidate.Engine == "podman" {
			endpoint = candidate
		}
	}
	if endpoint == nil {
		t.Fatal("validated rootless Podman endpoint required")
	}
	environment, err := rootlessContainerClientEnvironment(endpoint, openCodeDefaultToolPath)
	if err != nil {
		t.Fatal(err)
	}
	args := append(rootlessEnginePrefix(endpoint), "ps", "-aq", "--filter", "label=mcp.devbox.toolbox")
	control := exec.CommandContext(t.Context(), endpoint.Executable, args...)
	control.Env = environment
	output, err := control.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "read-only file system") {
		t.Fatalf("unfixed control did not reproduce read-only failure: %v", err)
	}
	if _, err := (execContainerCommandRunner{}).Run(t.Context(), endpoint.Executable, args, environment); err != nil {
		t.Fatalf("managed client failed in cold runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "libpod")); !os.IsNotExist(err) {
		t.Fatalf("host runtime was populated: %v", err)
	}
}
