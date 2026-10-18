//go:build !windows

package edgeclient

import (
	"io"
	"path/filepath"
	"testing"
)

func TestLinuxWorkcellProcessSpecAllowsOnlyRootlessContainerSocket(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	prepared.RootlessContainer = &RootlessContainerEndpoint{
		Engine:     "docker",
		SocketPath: "/run/user/1000/docker.sock",
		Executable: "/usr/bin/docker",
	}
	spec, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Sandbox.Environment["DOCKER_HOST"] != "unix://"+rootlessContainerSocketTarget || spec.Sandbox.Environment["MCP_DEVBOX_CONTAINER_LABEL"] != rootlessRuntimeLabelKey+"="+lease.RuntimeID {
		t.Fatalf("rootless env=%+v", spec.Sandbox.Environment)
	}
	mount, found := openCodeSandboxMount{}, false
	for _, candidate := range spec.Sandbox.Mounts {
		if candidate.Target == rootlessContainerSocketTarget {
			mount, found = candidate, true
		}
	}
	if !found || mount.Source != "/run/user/1000/docker.sock" || !mount.Writable {
		t.Fatalf("rootless mount=%+v found=%t", mount, found)
	}
}

func TestLinuxWorkcellProcessSpecRejectsRootfulDockerSocket(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	prepared.RootlessContainer = &RootlessContainerEndpoint{
		Engine:     "docker",
		SocketPath: "/var/run/docker.sock",
		Executable: "/usr/bin/docker",
	}
	if _, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard); err == nil {
		t.Fatal("rootful Docker socket accepted")
	}
}
