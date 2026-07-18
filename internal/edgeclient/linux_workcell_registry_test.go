package edgeclient

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func newLinuxWorkcellRegistry(t *testing.T) (*WorkspaceRegistry, string, string) {
	t.Helper()
	home := t.TempDir()
	devRoot := filepath.Join(home, "workspaces")
	htbRoot := filepath.Join(home, "htb-machines")
	for _, path := range []string{devRoot, htbRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := OpenWorkspaceRegistryWithRoots(t.TempDir(), WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry, devRoot, htbRoot
}

func TestWorkspaceRegistryLinuxWorkcellDefaultsToDevAndPersistsTypedMetadata(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	project := filepath.Join(devRoot, "example")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}

	entry, created, err := registry.AddProfile(project, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	if !created || entry.Profile != WorkspaceProfileLinuxWorkcell || entry.Mode != WorkspaceModeDev {
		t.Fatalf("entry=%+v created=%t", entry, created)
	}
	resolved, err := registry.Get(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != project || resolved.Profile != WorkspaceProfileLinuxWorkcell || resolved.Mode != WorkspaceModeDev {
		t.Fatalf("resolved=%+v", resolved)
	}
}

func TestWorkspaceRegistryLinuxWorkcellRejectsPathsOutsideRegisteredRoots(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	outside := filepath.Join(filepath.Dir(devRoot), "other", "project")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.AddProfile(outside, WorkspaceProfileLinuxWorkcell); err == nil {
		t.Fatal("linux-workcell accepted a path outside its local roots")
	}
}

func TestWorkspaceRegistryConfiguresHTBLinuxContext(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	machinePath := filepath.Join(htbRoot, "paperwork")
	if err := os.Mkdir(machinePath, 0o700); err != nil {
		t.Fatal(err)
	}
	entry, _, err := registry.AddProfile(machinePath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := registry.Configure(entry.ID, WorkspaceConfiguration{
		Mode:         WorkspaceModeHTBLinux,
		MachineName:  "Paperwork",
		TargetIP:     net.ParseIP("10.10.11.250").String(),
		Difficulty:   "EASY",
		OS:           "LINUX",
		VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.Mode != WorkspaceModeHTBLinux || configured.MachineName != "Paperwork" || configured.TargetIP != "10.10.11.250" || configured.VPNInterface != "tun0" {
		t.Fatalf("configured=%+v", configured)
	}
}

func TestWorkspaceRegistryRejectsInvalidHTBConfiguration(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	machinePath := filepath.Join(htbRoot, "fixture")
	if err := os.Mkdir(machinePath, 0o700); err != nil {
		t.Fatal(err)
	}
	entry, _, err := registry.AddProfile(machinePath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []WorkspaceConfiguration{
		{Mode: WorkspaceModeHTBLinux, MachineName: "Fixture", TargetIP: "10.10.10.0/24", Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0"},
		{Mode: WorkspaceModeHTBLinux, MachineName: "Fixture", TargetIP: "10.10.10.10", Difficulty: "IMPOSSIBLE", OS: "LINUX", VPNInterface: "tun0"},
		{Mode: WorkspaceModeHTBLinux, MachineName: "Fixture", TargetIP: "10.10.10.10", Difficulty: "EASY", OS: "WINDOWS", VPNInterface: "tun0"},
	}
	for _, config := range invalid {
		if _, err := registry.Configure(entry.ID, config); err == nil {
			t.Fatalf("invalid HTB config accepted: %+v", config)
		}
	}
}
