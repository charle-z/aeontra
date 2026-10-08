//go:build !windows

package edgeclient

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestDevelopmentToolchainRequirementsDoNotClaimAvailability(t *testing.T) {
	readiness := ToolchainReadiness{
		Status: ToolchainEdgeRequired,
		Findings: []ToolchainReadinessFinding{
			{Manifest: "go.mod", Tool: "go", Status: ToolchainSupported, Pin: "1.26"},
			{Manifest: "Makefile", Tool: "make", Status: ToolchainSupported},
			{Manifest: "pom.xml", Tool: "java", Status: ToolchainEdgeRequired},
		},
	}
	requirements, err := DevelopmentRequirementsFromToolchainReadiness(readiness)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]development.CapabilityID, 0, len(requirements))
	for _, requirement := range requirements {
		got = append(got, requirement.ID)
	}
	for _, want := range []development.CapabilityID{"build.make", "toolchain.go.v1-26", "toolchain.java"} {
		if !slices.Contains(got, want) {
			t.Fatalf("requirements=%v missing %s", got, want)
		}
	}
}

func TestDevelopmentToolchainRequirementsRejectConflictAndRepoCondition(t *testing.T) {
	if _, err := DevelopmentRequirementsFromToolchainReadiness(ToolchainReadiness{Status: ToolchainPinConflict}); err == nil {
		t.Fatal("pin conflict became a capability requirement")
	}
	_, err := DevelopmentRequirementsFromToolchainReadiness(ToolchainReadiness{
		Status: ToolchainEdgeRequired,
		Findings: []ToolchainReadinessFinding{{
			Manifest: "package.json", Tool: "package-manager", Status: ToolchainEdgeRequired,
		}},
	})
	if err == nil {
		t.Fatal("missing package-manager contract became environment capability")
	}
}

func TestDevelopmentCorepackPackageManagerRequirements(t *testing.T) {
	workspace := t.TempDir()
	writeToolchainFixture(t, workspace, map[string]string{"package.json": `{"packageManager":"` + corepackPnpmFixture + `"}`})
	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := DevelopmentRequirementsFromToolchainReadiness(readiness)
	if err != nil || len(requirements) != 1 || requirements[0].ID != "toolchain.pnpm.v11-19-0" || readiness.Status != ToolchainEdgeRequired {
		t.Fatalf("Corepack generic capability lost: %+v %+v %v", readiness, requirements, err)
	}
	for _, finding := range readiness.Findings {
		if finding.Tool == "pnpm" && finding.Pin != "11.19.0" {
			t.Fatalf("integrity suffix reached bounded findings: %+v", finding)
		}
	}
}

func TestDevelopmentCorepackInvalidIntegrityRemainsUnresolved(t *testing.T) {
	hash := strings.Repeat("a", 128)
	for _, version := range []string{
		"11.19.0+sha512." + hash[:127], "11.19.0+sha512." + hash + "a",
		"11.19.0+sha512." + hash[:127] + "g", "11.19.0+sha1." + strings.Repeat("a", 40),
		"11.19.0+SHA512." + hash, "11.19.0+sha512", "11.19.0+sha512." + hash + "+sha512." + hash,
		"11.19.0+sha512." + hash + ".extra", "11.19.0+sha512." + hash[:64] + " " + hash[65:],
		"^11.19.0+sha512." + hash, "11.19+sha512." + hash, "v11.19.0+sha512." + hash,
		"011.19.0+sha512." + hash, "11.19.0.1+sha512." + hash,
		"https://example.test/pnpm.js+sha512." + hash,
	} {
		for _, manager := range []string{"pnpm", "npm", "yarn"} {
			t.Run(manager+"@"+version, func(t *testing.T) {
				workspace := t.TempDir()
				writeToolchainFixture(t, workspace, map[string]string{"package.json": `{"packageManager":"` + manager + "@" + version + `"}`})
				readiness, err := DetectToolchainReadiness(workspace)
				if err != nil {
					t.Fatal(err)
				}
				if requirements, err := DevelopmentRequirementsFromToolchainReadiness(readiness); err == nil {
					t.Fatalf("invalid integrity became requirements: %+v", requirements)
				}
			})
		}
	}
	for _, version := range []string{"^11.19.0", "https://example.test/pnpm.js", "11.19.0+build"} {
		workspace := t.TempDir()
		writeToolchainFixture(t, workspace, map[string]string{"package.json": `{"packageManager":"pnpm@` + version + `"}`})
		readiness, err := DetectToolchainReadiness(workspace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DevelopmentRequirementsFromToolchainReadiness(readiness); err == nil {
			t.Fatalf("non-exact package-manager version accepted: %q", version)
		}
	}
}

func TestDevelopmentWorkcellAttestationUsesOnlyObservedAvailableTools(t *testing.T) {
	preparation := LinuxWorkcellPreparation{
		Workspace: Workspace{
			ID: "ws_11111111111111111111111111111111", Path: "/workspace/project",
			Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
		},
	}
	inventory := []LinuxToolInventoryEntry{
		{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"},
		{Name: "make", Available: true, Version: "4.4", Capability: "build-tool"},
		{Name: "git", Available: true, Version: "2.47.2", Capability: "version-control"},
		{Name: "pnpm", Available: false, Version: "absent", Capability: "node-packages"},
		{Name: "javac", Available: false, Version: "absent", Capability: "java-compiler"},
	}
	attestation, err := DevelopmentWorkcellAttestation("workcell-project", 9, preparation, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Class != development.ClassWorkcell ||
		!attestation.Capabilities.Has("network.host-shared") ||
		!attestation.Capabilities.Has("git.cli") ||
		!attestation.Capabilities.Has("toolchain.go") ||
		!attestation.Capabilities.Has("toolchain.go.v1-26") ||
		!attestation.Capabilities.Has("build.make") ||
		attestation.Capabilities.Has("toolchain.pnpm") ||
		attestation.Capabilities.Has("toolchain.java") ||
		attestation.Capabilities.Has("container.engine.rootless") {
		t.Fatalf("unexpected workcell attestation: %+v", attestation)
	}
}

func TestDevelopmentRootlessRuntimeAttestationIsSeparateFromWorkcell(t *testing.T) {
	preparation := LinuxWorkcellPreparation{
		Workspace: Workspace{
			ID: "ws_22222222222222222222222222222222", Path: "/workspace/project",
			Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
		},
		RootlessContainer:    &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"},
		ContainerProxySocket: "/runtime/docker-proxy.sock",
	}
	inventory := []LinuxToolInventoryEntry{
		{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"},
	}
	attestation, err := DevelopmentRootlessRuntimeAttestation("rootless-project", 4, preparation, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Class != development.ClassRootlessRuntime ||
		!attestation.Capabilities.Has("container.engine.rootless") ||
		!attestation.Capabilities.Has("container.docker.rootless") ||
		!attestation.Capabilities.Has("filesystem.workspace-bind") {
		t.Fatalf("unexpected rootless runtime attestation: %+v", attestation)
	}

	preparation.ContainerProxySocket = ""
	withoutBind, err := DevelopmentRootlessRuntimeAttestation("rootless-project-no-bind", 4, preparation, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if withoutBind.Capabilities.Has("filesystem.workspace-bind") {
		t.Fatal("workspace bind capability was inferred without the proxy contract")
	}
}

func TestDevelopmentToolboxAttestationDoesNotInventInstalledToolchains(t *testing.T) {
	exit := 0
	snapshot := ProjectToolboxSnapshot{
		ToolboxID: "tb_11111111111111111111111111111111",
		State:     ProjectToolboxRunning, Lifecycle: projectToolboxPersistent, Generation: 3,
		BaseImage: projectToolboxBaseImage, BaseImageID: "sha256:" + strings.Repeat("a", 64),
		CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048,
		ContainerAccess: true, ExitCode: &exit,
	}
	attestation, err := DevelopmentToolboxAttestation("toolbox-project", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Class != development.ClassToolbox ||
		!attestation.Capabilities.Has("dependency.install") ||
		!attestation.Capabilities.Has("service.local") ||
		!attestation.Capabilities.Has("container.engine.rootless") ||
		attestation.Capabilities.Has("toolchain.java") ||
		attestation.Capabilities.Has("toolchain.pnpm") {
		t.Fatalf("unexpected toolbox attestation: %+v", attestation)
	}
	snapshot.State = ProjectToolboxStopped
	if _, err := DevelopmentToolboxAttestation("toolbox-stopped", snapshot); err == nil {
		t.Fatal("stopped toolbox was attested as executable")
	}
}

func TestDevelopmentWorkcellAttestationRejectsHTBModeAndUnsafeInventory(t *testing.T) {
	preparation := LinuxWorkcellPreparation{
		Workspace: Workspace{
			ID: "ws_33333333333333333333333333333333", Path: "/workspace/project",
			Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeHTBLinux,
		},
	}
	if _, err := DevelopmentWorkcellAttestation("workcell-htb", 1, preparation, nil); err == nil {
		t.Fatal("HTB workcell entered development routing")
	}

	preparation.Workspace.Mode = WorkspaceModeDev
	inventory := []LinuxToolInventoryEntry{
		{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"},
		{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"},
	}
	if _, err := DevelopmentWorkcellAttestation("workcell-duplicate", 1, preparation, inventory); err == nil {
		t.Fatal("unsafe duplicate inventory was attested")
	}
}

func TestDevelopmentCodexRootlessRuntimeAttestsBuildxOnlyFromValidatedBundle(t *testing.T) {
	preparation := LinuxWorkcellPreparation{
		Workspace: Workspace{
			ID: "ws_44444444444444444444444444444444", Path: "/workspace/project",
			Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev,
		},
		RootlessContainer:    &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"},
		ContainerProxySocket: "/runtime/docker-proxy.sock",
	}
	inventory := []LinuxToolInventoryEntry{
		{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"},
	}
	root := t.TempDir()
	codexPath := filepath.Join(root, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"bin/docker", "config/cli-plugins/docker-buildx"} {
		path := filepath.Join(root, "container-tools", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	attestation, err := DevelopmentCodexRootlessRuntimeAttestation("codex-rootless", 8, preparation, inventory, codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !attestation.Capabilities.Has("container.docker.client") ||
		!attestation.Capabilities.Has("container.buildx") ||
		attestation.Capabilities.Has("git.cli") {
		t.Fatalf("signed container clients were not attested: %+v", attestation)
	}

	if err := os.Remove(filepath.Join(root, "container-tools", "config", "cli-plugins", "docker-buildx")); err != nil {
		t.Fatal(err)
	}
	if _, err := DevelopmentCodexRootlessRuntimeAttestation("codex-rootless-broken", 9, preparation, inventory, codexPath); err == nil {
		t.Fatal("incomplete signed container client bundle was attested")
	}
}

func TestDevelopmentToolchainRequirementsUseProvenBaselineForSupportedRange(t *testing.T) {
	requirements, err := DevelopmentRequirementsFromToolchainReadiness(ToolchainReadiness{
		Status: ToolchainSupported,
		Findings: []ToolchainReadinessFinding{{
			Manifest: "pyproject.toml", Tool: "python", Status: ToolchainSupported, Pin: ">=3.12,<3.15",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || requirements[0].ID != "toolchain.python.v3-14" {
		t.Fatalf("supported Python range requirements=%+v", requirements)
	}

	if _, err := DevelopmentRequirementsFromToolchainReadiness(ToolchainReadiness{
		Status: ToolchainEdgeRequired,
		Findings: []ToolchainReadinessFinding{{
			Manifest: "pyproject.toml", Tool: "python", Status: ToolchainEdgeRequired, Pin: ">=3.15",
		}},
	}); err == nil {
		t.Fatal("unresolved Edge-required version range degraded to an unversioned capability")
	}
}

func TestDevelopmentToolboxAttestationRejectsResourceAndImageDrift(t *testing.T) {
	base := ProjectToolboxSnapshot{
		ToolboxID: "tb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		State:     ProjectToolboxRunning, Lifecycle: projectToolboxPersistent, Generation: 2,
		BaseImage: projectToolboxBaseImage, BaseImageID: "sha256:" + strings.Repeat("b", 64),
		CPUMillis: 2000, MemoryMiB: 4096, ProcessLimit: 1024,
	}
	for name, mutate := range map[string]func(*ProjectToolboxSnapshot){
		"base image": func(value *ProjectToolboxSnapshot) { value.BaseImage = "docker.io/library/ubuntu:latest" },
		"cpu":        func(value *ProjectToolboxSnapshot) { value.CPUMillis = 0 },
		"memory":     func(value *ProjectToolboxSnapshot) { value.MemoryMiB = 0 },
		"pids":       func(value *ProjectToolboxSnapshot) { value.ProcessLimit = 0 },
		"generation": func(value *ProjectToolboxSnapshot) { value.Generation = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if _, err := DevelopmentToolboxAttestation("toolbox-drift-"+strings.ReplaceAll(name, " ", "-"), changed); err == nil {
				t.Fatal("drifted toolbox was attested")
			}
		})
	}
}
