//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"strings"
)

// CollectDevelopmentWorkcellInventory probes the same confined PATH that
// execution uses, including project-managed runtime toolchains. Host inventory
// alone cannot prove a tool installed below /runtime is available.
func CollectDevelopmentWorkcellInventory(ctx context.Context, request DirectWorkcellCommandRequest) ([]LinuxToolInventoryEntry, error) {
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
		script.WriteString("if (ulimit -f 4; timeout 2 " + definition.Executables[0] + " " + strings.Join(definition.VersionArgs, " ") + " >\"$tmp\" 2>/dev/null); then printf '" + definition.Name + "\\t'; head -c 1024 \"$tmp\" | tr '\\n\\r\\t' '   '; printf '\\n'; fi\n")
	}
	request.Argv = []string{"sh", "-c", script.String()}
	request.Environment = nil
	request.Stdin = ""
	request.CWD = ""
	request.TimeoutSeconds = 30
	result, err := RunDirectWorkcellCommand(ctx, request, nil)
	if err != nil || result.ExitCode != 0 || result.StdoutTruncated || result.TimedOut {
		return nil, errors.New("development workcell inventory unavailable")
	}
	return parseDevelopmentWorkcellInventory(result.Stdout, definitions)
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
		version := safeToolVersionPattern.FindString(value)
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
