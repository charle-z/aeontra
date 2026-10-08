package tools

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

func TestDevelopmentSandboxAttestationRequiresCompleteL3Contract(t *testing.T) {
	status := SandboxStatusInfo{
		Available: true, Backend: sandboxprotocol.Backend, DefaultEgress: "deny",
		FreeTerminal: true, ContainerReady: true, ExecReady: true, FilesystemReady: true,
		GitReady: true, NetworkPolicy: "loaded:deny", ToolchainState: "core-ready",
	}
	attestation, err := DevelopmentSandboxAttestation("l3-primary", 5, status)
	if err != nil {
		t.Fatal(err)
	}
	if attestation.Class != development.ClassL3Sandbox ||
		!attestation.Capabilities.Has("network.none") ||
		!attestation.Capabilities.Has("toolchain.go") ||
		!attestation.Capabilities.Has("toolchain.go.v1-26") ||
		!attestation.Capabilities.Has("build.make") ||
		attestation.Capabilities.Has("network.host-shared") ||
		attestation.Capabilities.Has("container.engine.rootless") {
		t.Fatalf("unexpected L3 attestation: %+v", attestation)
	}

	for name, mutate := range map[string]func(*SandboxStatusInfo){
		"unavailable":      func(value *SandboxStatusInfo) { value.Available = false },
		"wrong backend":    func(value *SandboxStatusInfo) { value.Backend = "docker" },
		"egress broadened": func(value *SandboxStatusInfo) { value.DefaultEgress = "allow" },
		"git missing":      func(value *SandboxStatusInfo) { value.GitReady = false },
		"network drift":    func(value *SandboxStatusInfo) { value.NetworkPolicy = "loaded:allow" },
		"toolchain drift":  func(value *SandboxStatusInfo) { value.ToolchainState = "partial" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := status
			mutate(&changed)
			if _, err := DevelopmentSandboxAttestation("l3-primary", 5, changed); err == nil {
				t.Fatal("drifted L3 status was attested")
			}
		})
	}
}
