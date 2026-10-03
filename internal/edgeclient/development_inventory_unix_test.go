//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

func runDevelopmentInventoryProbeFixture(t *testing.T, tools map[string]string) (string, []linuxToolDefinition, error) {
	t.Helper()
	toolPath := t.TempDir()
	for _, name := range []string{"timeout", "mktemp", "rm", "head", "tr", "sleep"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("required inventory fixture utility %s unavailable: %v", name, err)
		}
		if err := os.Symlink(path, filepath.Join(toolPath, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range tools {
		if err := os.WriteFile(filepath.Join(toolPath, name), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script, definitions := developmentWorkcellInventoryProbe()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	command.Env = []string{"PATH=" + toolPath, "TMPDIR=" + t.TempDir(), "HOME=/nonexistent", "LC_ALL=C"}
	output, err := command.Output()
	if ctx.Err() != nil {
		t.Fatalf("inventory probe exceeded fixture deadline: %v", ctx.Err())
	}
	return string(output), definitions, err
}

func TestDevelopmentInventoryProbeAbsentToolsRemainAbsent(t *testing.T) {
	output, definitions, err := runDevelopmentInventoryProbeFixture(t, nil)
	if err != nil {
		t.Fatalf("absent tools caused measurement failure: %v", err)
	}
	entries, err := parseDevelopmentWorkcellInventory(output, definitions)
	if err != nil || len(entries) != len(definitions) {
		t.Fatalf("inventory=%+v err=%v", entries, err)
	}
	for _, entry := range entries {
		if entry.Available || entry.Version != "absent" {
			t.Fatalf("absent tool advertised: %+v", entry)
		}
	}
}

func TestDevelopmentInventoryProbePresentMeasurementFailure(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		tools map[string]string
	}{
		{name: "nonzero", tools: map[string]string{"go": "printf 'go version go1.26.6 linux/amd64\\n'\nexit 1\n"}},
		{name: "timeout", tools: map[string]string{"go": "exec sleep 8\n"}},
		{name: "partial", tools: map[string]string{
			"go":    "printf 'go version go1.26.6 linux/amd64\\n'\n",
			"rustc": "exit 1\n",
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			output, definitions, err := runDevelopmentInventoryProbeFixture(t, fixture.tools)
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != developmentWorkcellInventoryMeasurementFailureExitCode {
				t.Fatalf("present tool must fail measurement, not become absent: output=%q err=%v", output, err)
			}
			entries, inventoryErr := developmentWorkcellInventoryFromResult(DirectWorkcellCommandResult{ExitCode: exitError.ExitCode(), Stdout: output}, definitions)
			if entries != nil || !errors.Is(inventoryErr, ErrDevelopmentWorkcellInventoryMeasurementFailed) {
				t.Fatalf("failed measurement returned partial inventory or untyped error: inventory=%+v err=%v", entries, inventoryErr)
			}
		})
	}
}

func TestDevelopmentInventoryProbeTimeoutBudget(t *testing.T) {
	script, definitions := developmentWorkcellInventoryProbe()
	if developmentWorkcellInventoryProbeTimeoutSeconds != 5 || developmentWorkcellInventoryTimeoutSeconds != 60 {
		t.Fatal("inventory timeout contract changed")
	}
	if strings.Count(script, "timeout 5 ") != len(definitions) {
		t.Fatal("generated probes do not use the bounded per-tool ceiling")
	}
	if len(definitions)*developmentWorkcellInventoryProbeTimeoutSeconds >= developmentWorkcellInventoryTimeoutSeconds {
		t.Fatal("aggregate inventory deadline leaves no probe overhead budget")
	}
}

func TestDevelopmentInventoryProbePreservesMeasuredCapabilities(t *testing.T) {
	output, definitions, err := runDevelopmentInventoryProbeFixture(t, map[string]string{
		"go":    "printf 'go version go1.26.6 linux/amd64\\n'\n",
		"rustc": "printf 'rustc 1.95.0 (example)\\n'\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseDevelopmentWorkcellInventory(output, definitions)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := developmentCapabilitiesFromLinuxInventory(entries)
	if err != nil || !caps.Has("toolchain.go.v1-26-6") || !caps.Has("toolchain.rust.v1-95-0") || caps.Has("toolchain.rust.v1-95-1") || caps.Has("toolchain.node") {
		t.Fatalf("capabilities=%v err=%v", caps.IDs(), err)
	}
}

func TestDevelopmentInventoryProbeAcceptsBoundedColdStartup(t *testing.T) {
	output, definitions, err := runDevelopmentInventoryProbeFixture(t, map[string]string{
		"rustc": "sleep 3\nprintf 'rustc 1.95.0 (example)\\n'\n",
	})
	if err != nil {
		t.Fatalf("bounded cold startup failed measurement: %v", err)
	}
	entries, err := parseDevelopmentWorkcellInventory(output, definitions)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := developmentCapabilitiesFromLinuxInventory(entries)
	if err != nil || !caps.Has("toolchain.rust.v1-95-0") {
		t.Fatalf("capabilities=%v err=%v", caps.IDs(), err)
	}
}

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

func TestDevelopmentInventoryGoVersionFormat(t *testing.T) {
	definitions := []linuxToolDefinition{{Name: "go", Capability: "go-toolchain"}}
	for _, fixture := range []struct {
		output  string
		version string
	}{
		{output: "go version go1.26.6 linux/amd64", version: "1.26.6"},
		{output: "go version gotip linux/amd64", version: "unknown"},
		{output: "go version goinvalid1.26.6 linux/amd64", version: "unknown"},
		{output: "go version go1.26.6invalid linux/amd64", version: "unknown"},
	} {
		entries, err := parseDevelopmentWorkcellInventory("go\t"+fixture.output+"\n", definitions)
		if err != nil || len(entries) != 1 || !entries[0].Available || entries[0].Version != fixture.version {
			t.Fatalf("output=%q inventory=%+v err=%v", fixture.output, entries, err)
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
