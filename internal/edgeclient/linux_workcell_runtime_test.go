package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLinuxNetworkProbe struct {
	ipv4           string
	routeInterface string
	interfaceErr   error
	routeErr       error
}

func (p fakeLinuxNetworkProbe) InterfaceIPv4(context.Context, string) (string, error) {
	return p.ipv4, p.interfaceErr
}

func (p fakeLinuxNetworkProbe) RouteInterface(context.Context, string) (string, error) {
	return p.routeInterface, p.routeErr
}

func runtimeLeaseFor(workspace Workspace, goal string) ModelRuntimeLease {
	return ModelRuntimeLease{
		RuntimeID:      "mr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkspaceID:    workspace.ID,
		Goal:           goal,
		TimeoutSeconds: 3600,
	}
}

func TestPrepareLinuxWorkcellDevCreatesPrivateDurableFiles(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	path := filepath.Join(devRoot, "project")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "Run tests and fix the failure."), nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.LHOST != "" || prepared.ResumeState == "" {
		t.Fatalf("prepared=%+v", prepared)
	}
	for _, path := range []string{
		filepath.Join(workspace.Path, ".mcp-devbox"),
		filepath.Join(workspace.Path, ".mcp-devbox", "tools"),
		filepath.Join(workspace.Path, ".mcp-devbox", "cache"),
		filepath.Join(workspace.Path, ".mcp-devbox", "runtime"),
	} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("private dir %s mode=%v err=%v", path, infoMode(info), err)
		}
	}
	instructions, err := os.ReadFile(prepared.InstructionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(instructions), "Mode: dev") || !strings.Contains(string(instructions), "Run tests and fix the failure.") || strings.Contains(string(instructions), "writeups") {
		t.Fatalf("instructions=%s", instructions)
	}
	info, err := os.Stat(prepared.InstructionsPath)
	if err != nil || info.Mode().Perm() != 0o400 {
		t.Fatalf("instructions mode=%v err=%v", infoMode(info), err)
	}
	stateInfo, err := os.Stat(prepared.CurrentStatePath)
	if err != nil || stateInfo.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%v err=%v", infoMode(stateInfo), err)
	}
}

func TestPrepareLinuxWorkcellHTBRendersTemplateAndStructure(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	path := filepath.Join(htbRoot, "paperwork")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Paperwork", TargetIP: "10.10.11.250",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	probe := fakeLinuxNetworkProbe{ipv4: "10.10.14.25", routeInterface: "tun0"}
	prepared, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "Solve the authorized Linux room."), probe)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.LHOST != "10.10.14.25" {
		t.Fatalf("LHOST=%q", prepared.LHOST)
	}
	for _, name := range []string{"scans", "loot", "scripts", "reports", "tmp", "tickets"} {
		info, err := os.Stat(filepath.Join(workspace.Path, name))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("HTB dir %s mode=%v err=%v", name, infoMode(info), err)
		}
	}
	instructions, err := os.ReadFile(prepared.InstructionsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(instructions)
	for _, expected := range []string{"Paperwork", "10.10.11.250", "10.10.14.25", "Recon corto", "Anti-loop", "Máquina recién publicada", "No consultar writeups"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("instructions missing %q", expected)
		}
	}
	if strings.Contains(text, "{{") {
		t.Fatalf("unrendered placeholder: %s", text)
	}
}

func TestPrepareLinuxWorkcellHTBPreflightFailsClosedBeforeWriting(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	path := filepath.Join(htbRoot, "fixture")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Fixture", TargetIP: "10.10.10.10",
		Difficulty: "MEDIUM", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	badProbes := []fakeLinuxNetworkProbe{
		{interfaceErr: errors.New("missing")},
		{ipv4: "10.10.14.25", routeInterface: "eth0"},
		{ipv4: "not-an-ip", routeInterface: "tun0"},
	}
	for _, probe := range badProbes {
		if _, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "fixture"), probe); err == nil {
			t.Fatalf("preflight accepted probe=%+v", probe)
		}
		if _, err := os.Stat(filepath.Join(path, ".mcp-devbox")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("preflight wrote state before validation: %v", err)
		}
	}
}

func TestPrepareLinuxWorkcellResumesExistingStateWithoutOverwriting(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	path := filepath.Join(devRoot, "resume")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	first, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "first"), nil)
	if err != nil {
		t.Fatal(err)
	}
	const checkpoint = "# Current State\n\n- Tests: passed\n- Next action: build image\n"
	if err := WriteLinuxWorkcellState(first.CurrentStatePath, checkpoint); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "continue"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ResumeState != checkpoint {
		t.Fatalf("resume=%q", second.ResumeState)
	}
	content, err := os.ReadFile(second.CurrentStatePath)
	if err != nil || string(content) != checkpoint {
		t.Fatalf("state=%q err=%v", content, err)
	}
}

func TestPrepareLinuxWorkcellRejectsSymlinkedControlDirectory(t *testing.T) {
	registry, devRoot, _ := newLinuxWorkcellRegistry(t)
	path := filepath.Join(devRoot, "unsafe")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(path, ".mcp-devbox")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "unsafe"), nil); err == nil {
		t.Fatal("symlinked control directory accepted")
	}
}

func TestPrepareLinuxWorkcellHTBRedactsCheckpointOnlyInModelInstructions(t *testing.T) {
	registry, _, htbRoot := newLinuxWorkcellRegistry(t)
	path := filepath.Join(htbRoot, "checkpoint")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := registry.AddProfile(path, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err = registry.Configure(workspace.ID, WorkspaceConfiguration{
		Mode: WorkspaceModeHTBLinux, MachineName: "Checkpoint", TargetIP: "10.10.10.10",
		Difficulty: "EASY", OS: "LINUX", VPNInterface: "tun0",
	})
	if err != nil {
		t.Fatal(err)
	}
	probe := fakeLinuxNetworkProbe{ipv4: "10.10.14.25", routeInterface: "tun0"}
	first, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "first"), probe)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := "# Current State\n\n- Password: local-lab-password\n- Credential handle: source=loot/capture.txt prefix=PASS\n- user.txt: 0123456789abcdef0123456789abcdef\n"
	if err := WriteLinuxWorkcellState(first.CurrentStatePath, checkpoint); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareLinuxWorkcell(context.Background(), workspace, runtimeLeaseFor(workspace, "continue"), probe)
	if err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(second.CurrentStatePath)
	if err != nil || string(local) != checkpoint {
		t.Fatalf("local checkpoint=%q err=%v", local, err)
	}
	instructions, err := os.ReadFile(second.InstructionsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(instructions)
	if strings.Contains(text, "local-lab-password") || strings.Contains(text, "0123456789abcdef0123456789abcdef") {
		t.Fatalf("instructions leaked checkpoint: %s", text)
	}
	if !strings.Contains(text, "source=loot/capture.txt prefix=PASS") || !strings.Contains(text, "[LOCAL-ONLY-VALUE]") {
		t.Fatalf("instructions lost handle/redaction: %s", text)
	}
}
