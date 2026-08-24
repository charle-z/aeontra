//go:build !windows

package sandboxexecutor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPodmanEngineAgainstRealRootlessAPI(t *testing.T) {
	if os.Getenv("MCP_DEVBOX_PODMAN_E2E") != "1" {
		t.Skip("set MCP_DEVBOX_PODMAN_E2E=1 for real rootless Podman acceptance")
	}
	socket := os.Getenv("MCP_DEVBOX_PODMAN_E2E_SOCKET")
	image := os.Getenv("MCP_DEVBOX_PODMAN_E2E_IMAGE")
	digest := os.Getenv("MCP_DEVBOX_PODMAN_E2E_DIGEST")
	workspace := os.Getenv("MCP_DEVBOX_PODMAN_E2E_WORKSPACE")
	if socket == "" || image == "" || digest == "" || workspace == "" {
		t.Fatal("real Podman acceptance environment is incomplete")
	}
	engine, err := NewPodmanEngine(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Attest(context.Background(), image, digest); err != nil {
		t.Fatal(err)
	}
	response, err := engine.Run(context.Background(), RunSpec{
		WorkspaceRoot: workspace, Argv: []string{"/probe"}, NetworkProfile: "none",
		Timeout: 10 * time.Second, CPUMillis: 500, MemoryMiB: 128, ProcessLimit: 32,
		OutputBytes: 4096, Image: image,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.ExitCode != 0 || response.Truncated || strings.TrimSpace(response.Stdout) != "probe-stdout" || strings.TrimSpace(response.Stderr) != "probe-stderr" {
		t.Fatalf("unexpected real Podman response: %#v", response)
	}
	written, err := os.ReadFile(filepath.Join(workspace, "probe.txt"))
	if err != nil || string(written) != "workspace-write-ok\n" {
		t.Fatalf("workspace write was not preserved: content=%q err=%v", written, err)
	}
}
