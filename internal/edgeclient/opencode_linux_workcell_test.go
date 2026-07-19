//go:build !windows

package edgeclient

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linuxWorkcellLauncherFixture(t *testing.T, mode WorkspaceMode) (*openCodeLauncherFixture, Workspace, LinuxWorkcellPreparation, ModelRuntimeLease, string) {
	t.Helper()
	fixture := newOpenCodeLauncherFixture(t)
	home := t.TempDir()
	devRoot := filepath.Join(home, "workspaces")
	htbRoot := filepath.Join(home, "htb-machines")
	for _, root := range []string{devRoot, htbRoot} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture.registry.roots = WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot}
	root := devRoot
	name := "project"
	if mode == WorkspaceModeHTBLinux {
		root = htbRoot
		name = "fixture"
	}
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := fixture.registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	if mode == WorkspaceModeHTBLinux {
		workspace, err = fixture.registry.Configure(workspace.ID, WorkspaceConfiguration{
			Mode: WorkspaceModeHTBLinux, MachineName: "Fixture", TargetIP: "10.10.10.10",
			Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	lease := fixture.lease
	lease.WorkspaceID = workspace.ID
	probe := LinuxNetworkProbe(nil)
	if mode == WorkspaceModeHTBLinux {
		probe = fakeLinuxNetworkProbe{ipv4: "10.10.14.9", routeInterface: "tun0"}
	}
	prepared, err := PrepareLinuxWorkcell(context.Background(), workspace, lease, probe)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(fixture.state, "r", "linux-workcell")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return fixture, workspace, prepared, lease, runtimeDir
}

func TestLinuxWorkcellProcessSpecSharesOnlyHostNetworkAndUsesPersistentPrefixes(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	spec, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Sandbox.UnshareAll || !spec.Sandbox.ShareNetwork || !spec.Sandbox.ClearEnv {
		t.Fatalf("namespace posture=%+v", spec.Sandbox)
	}
	if spec.Sandbox.Command[len(spec.Sandbox.Command)-1] != linuxWorkcellOpenCodePrompt {
		t.Fatalf("command=%q", spec.Sandbox.Command)
	}
	environment := spec.Sandbox.Environment
	for key, expected := range map[string]string{
		"MCP_DEVBOX_PROFILE":         "linux-workcell",
		"MCP_DEVBOX_MODE":            "dev",
		"MCP_DEVBOX_NETWORK_POSTURE": LinuxWorkcellNetworkPosture,
		"MCP_DEVBOX_RUNTIME_ID":      lease.RuntimeID,
	} {
		if environment[key] != expected {
			t.Fatalf("%s=%q want %q", key, environment[key], expected)
		}
	}
	if !strings.HasPrefix(environment["PATH"], "/workspace/.mcp-devbox/tools/bin:") || environment["XDG_CACHE_HOME"] != "/workspace/.mcp-devbox/cache" {
		t.Fatalf("persistent environment=%+v", environment)
	}
	for _, mount := range spec.Sandbox.Mounts {
		if mount.Source == "/var/run/docker.sock" || mount.Source == "/run/docker.sock" ||
			mount.Source == "/mnt/c" || pathInside("/mnt/c", mount.Source) ||
			mount.Source == "/mnt/d" || pathInside("/mnt/d", mount.Source) {
			t.Fatalf("forbidden mount=%+v", mount)
		}
	}
}

func TestLinuxWorkcellProcessSpecRendersHTBEnvironmentLocally(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeHTBLinux)
	spec, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Sandbox.Environment["TARGET"] != "10.10.10.10" || spec.Sandbox.Environment["LHOST"] != "10.10.14.9" || spec.Sandbox.Environment["MCP_DEVBOX_MODE"] != "htb-linux" {
		t.Fatalf("HTB environment=%+v", spec.Sandbox.Environment)
	}
}

func TestSandboxValidatorRejectsHostSharedNetwork(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	runtimeDir := filepath.Join(fixture.state, "r", "sandbox-network")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	spec, err := fixture.launcher.processSpec(runtimeDir, fixture.workspace, filepath.Join(runtimeDir, openCodeDriverSocketName), fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	spec.Sandbox.ShareNetwork = true
	resolved, err := filepath.EvalSymlinks(fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateOpenCodeSandboxSpec(spec.Sandbox, fixture.state, runtimeDir, fixture.workspace, fixture.provider, resolved, openCodeDefaultToolPath, fixture.lease); err == nil {
		t.Fatal("sandbox accepted host-shared network")
	}
}
