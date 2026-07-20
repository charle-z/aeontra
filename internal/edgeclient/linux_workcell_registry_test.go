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

func TestWorkspaceRegistryRetargetPreservesIdentityAndEvidenceWhileRotatingAuthorization(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	defer registry.Close()
	path := filepath.Join(htbRoot, "Cap")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Cap", TargetIP: "10.129.63.164",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil || workspace.AuthorizationRevision != 1 {
		t.Fatalf("initial authorization = %+v, %v", workspace, err)
	}
	checkpoint := filepath.Join(path, "checkpoint.md")
	if err := os.WriteFile(checkpoint, []byte("preserved evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	retargeted, err := registry.Retarget(workspace.ID, "10.129.63.200", "tun1")
	if err != nil {
		t.Fatal(err)
	}
	if retargeted.ID != workspace.ID || retargeted.AuthorizationRevision != 2 || retargeted.TargetIP != "10.129.63.200" || retargeted.VPNInterface != "tun1" {
		t.Fatalf("unexpected retarget: %+v", retargeted)
	}
	if content, err := os.ReadFile(checkpoint); err != nil || string(content) != "preserved evidence" {
		t.Fatalf("checkpoint changed: %q, %v", content, err)
	}
	marker, err := readWorkspaceAuthorizationRevision(path)
	if err != nil || marker != 2 {
		t.Fatalf("authorization marker = %d, %v", marker, err)
	}
	again, err := registry.Retarget(workspace.ID, "10.129.63.200", "tun1")
	if err != nil || again.AuthorizationRevision != 2 {
		t.Fatalf("idempotent retarget = %+v, %v", again, err)
	}
}

func TestWorkspaceRegistryRejectsPublicHTBTarget(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	defer registry.Close()
	path := filepath.Join(htbRoot, "Unsafe")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Unsafe", TargetIP: "8.8.8.8",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err == nil {
		t.Fatal("public target was authorized")
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
