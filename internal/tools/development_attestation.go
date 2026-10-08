package tools

import (
	"errors"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

// DevelopmentSandboxAttestation converts an already authenticated L3 status
// result into the generic development capability model. Availability alone is
// insufficient: every containment property used by development routing must
// match the closed L3-v2 contract.
func DevelopmentSandboxAttestation(environmentID string, generation uint64, status SandboxStatusInfo) (development.EnvironmentAttestation, error) {
	if !status.Available ||
		status.Backend != sandboxprotocol.Backend ||
		status.DefaultEgress != "deny" ||
		!status.FreeTerminal ||
		!status.ContainerReady ||
		!status.ExecReady ||
		!status.FilesystemReady ||
		!status.GitReady ||
		status.NetworkPolicy != "loaded:deny" ||
		status.ToolchainState != "core-ready" {
		return development.EnvironmentAttestation{}, errors.New("development L3 sandbox attestation is unavailable")
	}
	names := []string{
		"build.make",
		"exec.argv",
		"filesystem.workspace-rw",
		"git.cli",
		"isolation.rootless",
		"network.none",
		"toolchain.c",
		"toolchain.cpp",
	}
	for _, versioned := range []struct {
		capability string
		version    string
	}{
		{capability: "toolchain.go", version: "1.26"},
		{capability: "toolchain.rust", version: "1.96"},
		{capability: "toolchain.python", version: "3.14"},
		{capability: "toolchain.node", version: "24"},
		{capability: "toolchain.npm", version: "12"},
	} {
		ids, err := development.VersionCapabilityIDs(versioned.capability, versioned.version)
		if err != nil {
			return development.EnvironmentAttestation{}, err
		}
		for _, id := range ids {
			names = append(names, string(id))
		}
	}
	capabilities, err := development.NewCapabilitySet(names...)
	if err != nil {
		return development.EnvironmentAttestation{}, err
	}
	return development.NewEnvironmentAttestation(environmentID, development.ClassL3Sandbox, generation, capabilities)
}
