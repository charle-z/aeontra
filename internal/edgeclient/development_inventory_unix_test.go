//go:build !windows

package edgeclient

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func TestDevelopmentInventoryPreservesExactZeroPatch(t *testing.T) {
	definitions := []linuxToolDefinition{{Name: "rust", Capability: "rust-toolchain"}, {Name: "go", Capability: "go-toolchain"}}
	entries, err := parseDevelopmentWorkcellInventory("rust\trustc 1.95.0 (example)\n", definitions)
	if err != nil || len(entries) != 2 || entries[0].Version != "1.95.0" || !entries[0].Available || entries[1].Available {
		t.Fatalf("inventory=%+v err=%v", entries, err)
	}
	caps, err := developmentCapabilitiesFromLinuxInventory(entries)
	if err != nil || !caps.Has("toolchain.rust.v1-95-0") || caps.Has("toolchain.rust.v1-95-1") {
		t.Fatalf("capabilities=%v err=%v", caps.IDs(), err)
	}
	required, err := DevelopmentRequirementsFromToolchainReadiness(ToolchainReadiness{Status: ToolchainEdgeRequired,
		Findings: []ToolchainReadinessFinding{{Tool: "rust", Pin: "1.95.0", Status: ToolchainEdgeRequired}}})
	if err != nil || len(required) != 1 || required[0].ID != development.CapabilityID("toolchain.rust.v1-95-0") {
		t.Fatalf("requirements=%v err=%v", required, err)
	}
}

func TestDevelopmentInventoryRejectsMalformedAndDuplicateEntries(t *testing.T) {
	definitions := []linuxToolDefinition{{Name: "go", Capability: "go-toolchain"}}
	for _, output := range []string{"go 1.26.6", "curl\t8.0.0", "go\t1.26.6\ngo\t1.26.6", "go\t1.26.6\x00"} {
		if _, err := parseDevelopmentWorkcellInventory(output, definitions); err == nil {
			t.Fatalf("accepted invalid output %q", output)
		}
	}
}

func TestDevelopmentWorkcellDoesNotInventGitAvailability(t *testing.T) {
	preparation := LinuxWorkcellPreparation{Workspace: Workspace{ID: "ws_11111111111111111111111111111111", Path: "/workspace/project", Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}}
	inventory := []LinuxToolInventoryEntry{{Name: "git", Available: false, Version: "absent", Capability: "git-tool"}}
	attestation, err := DevelopmentWorkcellAttestation("workcell:"+preparation.Workspace.ID, 1, preparation, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Capabilities.Has("git.cli") {
		t.Fatal("missing Git was advertised as available")
	}
}
