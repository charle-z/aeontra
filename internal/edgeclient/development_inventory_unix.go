//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	developmentWorkcellInventoryProbeTimeoutSeconds        = 5
	developmentWorkcellInventoryTimeoutSeconds             = 60
	developmentWorkcellInventoryMeasurementFailureExitCode = 126
)

// ErrDevelopmentWorkcellInventoryMeasurementFailed reports a present tool whose
// bounded version probe failed. It does not attest absence or partial inventory.
var ErrDevelopmentWorkcellInventoryMeasurementFailed = errors.New("development workcell inventory measurement failed")

// CollectDevelopmentWorkcellInventory probes the same confined PATH that
// execution uses, including project-managed runtime toolchains. Host inventory
// alone cannot prove a tool installed below /runtime is available.
func CollectDevelopmentWorkcellInventory(ctx context.Context, request DirectWorkcellCommandRequest) ([]LinuxToolInventoryEntry, error) {
	script, definitions := developmentWorkcellInventoryProbe()
	request.Argv = []string{"sh", "-c", script}
	request.Environment = nil
	request.Stdin = ""
	request.CWD = ""
	request.TimeoutSeconds = developmentWorkcellInventoryTimeoutSeconds
	result, err := RunDirectWorkcellCommand(ctx, request, nil)
	if err != nil {
		return nil, errors.New("development workcell inventory unavailable")
	}
	return developmentWorkcellInventoryFromResult(result, definitions)
}

func developmentWorkcellInventoryFromResult(result DirectWorkcellCommandResult, definitions []linuxToolDefinition) ([]LinuxToolInventoryEntry, error) {
	if result.ExitCode == developmentWorkcellInventoryMeasurementFailureExitCode && !result.StdoutTruncated && !result.TimedOut {
		return nil, ErrDevelopmentWorkcellInventoryMeasurementFailed
	}
	if result.ExitCode != 0 || result.StdoutTruncated || result.TimedOut {
		return nil, errors.New("development workcell inventory unavailable")
	}
	return parseDevelopmentWorkcellInventory(result.Stdout, definitions)
}

// developmentWorkcellInventoryProbe owns the fixed definitions and script used
// for confined inventory measurement, independently of workcell execution.
func developmentWorkcellInventoryProbe() (string, []linuxToolDefinition) {
	definitions := []linuxToolDefinition{
		{Name: "go", Executables: []string{"go"}, VersionArgs: []string{"version"}, Capability: "go-toolchain"},
		{Name: "rust", Executables: []string{"rustc"}, VersionArgs: []string{"--version"}, Capability: "rust-toolchain"},
		{Name: "cargo", Executables: []string{"cargo"}, VersionArgs: []string{"--version"}, Capability: "rust-packages"},
		{Name: "node", Executables: []string{"node"}, VersionArgs: []string{"--version"}, Capability: "node-runtime"},
		{Name: "npm", Executables: []string{"npm"}, VersionArgs: []string{"--version"}, Capability: "node-packages"},
		{Name: "pnpm", Executables: []string{"pnpm"}, VersionArgs: []string{"--version"}, Capability: "node-packages"},
		{Name: "python", Executables: []string{"python3"}, VersionArgs: []string{"--version"}, Capability: "python-runtime"},
		{Name: "make", Executables: []string{"make"}, VersionArgs: []string{"--version"}, Capability: "build-tool"},
		{Name: "git", Executables: []string{"git"}, VersionArgs: []string{"--version"}, Capability: "git-tool"},
	}
	var script strings.Builder
	script.WriteString("command -v timeout >/dev/null || exit 125\ntmp=$(mktemp) || exit 125\ntrap 'rm -f \"$tmp\"' EXIT HUP INT TERM\n")
	for _, definition := range definitions {
		// Every token is a server-owned literal from this fixed table.
		// Only lookup absence may omit a row. A failed measurement of a
		// present tool invalidates the whole snapshot, including earlier rows.
		script.WriteString("if command -v " + definition.Executables[0] + " >/dev/null 2>&1; then\n")
		fmt.Fprintf(&script, "if (ulimit -f 4; timeout %d %s %s >\"$tmp\" 2>/dev/null); then printf '%s\\t'; head -c 1024 \"$tmp\" | tr '\\n\\r\\t' '   '; printf '\\n'; else exit %d; fi\nfi\n",
			developmentWorkcellInventoryProbeTimeoutSeconds, definition.Executables[0], strings.Join(definition.VersionArgs, " "), definition.Name, developmentWorkcellInventoryMeasurementFailureExitCode)
	}
	return script.String(), definitions
}

func parseDevelopmentWorkcellInventory(output string, definitions []linuxToolDefinition) ([]LinuxToolInventoryEntry, error) {
	if len(output) > 24<<10 || strings.ContainsRune(output, 0) {
		return nil, errors.New("development inventory response invalid")
	}
	versions := make(map[string]string)
	known := make(map[string]bool)
	for _, definition := range definitions {
		known[definition.Name] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, "\t")
		if !ok || !known[name] || versions[name] != "" || len(value) > 1024 {
			return nil, errors.New("development inventory response invalid")
		}
		// Go embeds its numeric version directly after "go", which is not
		// a word boundary. Strip only its standard server-known output prefix.
		version := safeToolVersionPattern.FindString(value)
		if name == "go" && strings.HasPrefix(value, "go version go") {
			token, _, _ := strings.Cut(strings.TrimPrefix(value, "go version go"), " ")
			version = exactDevelopmentToolchainVersion(token)
		}
		if version == "" || len(version) > 64 {
			version = "unknown"
		}
		versions[name] = strings.TrimPrefix(strings.ToLower(version), "v")
	}
	entries := make([]LinuxToolInventoryEntry, 0, len(definitions))
	for _, definition := range definitions {
		version, present := versions[definition.Name]
		if !present {
			version = "absent"
		}
		entries = append(entries, LinuxToolInventoryEntry{Name: definition.Name, Available: present, Version: version, Capability: definition.Capability})
	}
	return entries, ValidateLinuxToolInventory(entries)
}
