package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type LinuxToolInventoryEntry struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	Version    string `json:"version"`
	Capability string `json:"capability"`
}

type linuxToolDefinition struct {
	Name        string
	Executables []string
	VersionArgs []string
	Capability  string
}

var safeToolVersionPattern = regexp.MustCompile(`(?i)\bv?\d+(?:\.\d+){0,3}(?:[-+._][a-z0-9]+)*\b`)

func CollectLinuxToolInventory(ctx context.Context, toolPath string) ([]LinuxToolInventoryEntry, error) {
	if strings.TrimSpace(toolPath) == "" {
		toolPath = openCodeDefaultToolPath
	}
	if err := validateOpenCodeToolPath(toolPath); err != nil {
		return nil, err
	}
	definitions := []linuxToolDefinition{
		{Name: "nmap", Executables: []string{"nmap"}, VersionArgs: []string{"--version"}, Capability: "network-recon"},
		{Name: "ssh", Executables: []string{"ssh"}, VersionArgs: []string{"-V"}, Capability: "target-locked-remote-access"},
		{Name: "curl", Executables: []string{"curl"}, VersionArgs: []string{"--version"}, Capability: "http-client"},
		{Name: "wget", Executables: []string{"wget"}, VersionArgs: []string{"--version"}, Capability: "download-client"},
		{Name: "openssl", Executables: []string{"openssl"}, VersionArgs: []string{"version"}, Capability: "tls-crypto"},
		{Name: "ffuf", Executables: []string{"ffuf"}, VersionArgs: []string{"-V"}, Capability: "web-fuzzing"},
		{Name: "feroxbuster", Executables: []string{"feroxbuster"}, VersionArgs: []string{"--version"}, Capability: "content-discovery"},
		{Name: "gobuster", Executables: []string{"gobuster"}, VersionArgs: []string{"version"}, Capability: "content-discovery"},
		{Name: "whatweb", Executables: []string{"whatweb"}, VersionArgs: []string{"--version"}, Capability: "web-fingerprinting"},
		{Name: "smbclient", Executables: []string{"smbclient"}, VersionArgs: []string{"--version"}, Capability: "smb-client"},
		{Name: "rpcclient", Executables: []string{"rpcclient"}, VersionArgs: []string{"--version"}, Capability: "rpc-client"},
		{Name: "ldapsearch", Executables: []string{"ldapsearch"}, VersionArgs: []string{"-VV"}, Capability: "ldap-client"},
		{Name: "impacket", Executables: []string{"impacket-smbserver", "impacket-psexec"}, VersionArgs: []string{"-h"}, Capability: "network-protocol-toolkit"},
		{Name: "netexec", Executables: []string{"netexec", "nxc"}, VersionArgs: []string{"--version"}, Capability: "network-service-enumeration"},
		{Name: "evil-winrm", Executables: []string{"evil-winrm"}, VersionArgs: []string{"--version"}, Capability: "winrm-client"},
		{Name: "john", Executables: []string{"john"}, VersionArgs: []string{"--list=build-info"}, Capability: "password-auditing"},
		{Name: "hashcat", Executables: []string{"hashcat"}, VersionArgs: []string{"--version"}, Capability: "password-auditing"},
		{Name: "hydra", Executables: []string{"hydra"}, VersionArgs: []string{"-h"}, Capability: "credential-validation"},
		{Name: "python", Executables: []string{"python3", "python"}, VersionArgs: []string{"--version"}, Capability: "python-runtime"},
		{Name: "gcc", Executables: []string{"gcc"}, VersionArgs: []string{"--version"}, Capability: "c-compiler"},
		{Name: "go", Executables: []string{"go"}, VersionArgs: []string{"version"}, Capability: "go-toolchain"},
		{Name: "node", Executables: []string{"node"}, VersionArgs: []string{"--version"}, Capability: "node-runtime"},
		{Name: "npm", Executables: []string{"npm"}, VersionArgs: []string{"--version"}, Capability: "node-packages"},
		{Name: "pnpm", Executables: []string{"pnpm"}, VersionArgs: []string{"--version"}, Capability: "node-packages"},
		{Name: "rust", Executables: []string{"rustc"}, VersionArgs: []string{"--version"}, Capability: "rust-toolchain"},
		{Name: "cargo", Executables: []string{"cargo"}, VersionArgs: []string{"--version"}, Capability: "rust-packages"},
		{Name: "docker", Executables: []string{"docker"}, VersionArgs: []string{"--version"}, Capability: "rootless-containers"},
		{Name: "podman", Executables: []string{"podman"}, VersionArgs: []string{"--version"}, Capability: "rootless-containers"},
	}
	entries := make([]LinuxToolInventoryEntry, 0, len(definitions))
	for _, definition := range definitions {
		entry := LinuxToolInventoryEntry{Name: definition.Name, Version: "absent", Capability: definition.Capability}
		path := ""
		for _, executable := range definition.Executables {
			if candidate, ok := findSafeLinuxTool(executable, toolPath); ok {
				path = candidate
				break
			}
		}
		if path == "" {
			entries = append(entries, entry)
			continue
		}
		entry.Available = true
		entry.Version = safeLinuxToolVersion(ctx, path, definition.VersionArgs, toolPath)
		entries = append(entries, entry)
	}
	return entries, nil
}

func findSafeLinuxTool(name, toolPath string) (string, bool) {
	if name == "" || strings.ContainsAny(name, `/\\`) {
		return "", false
	}
	for _, directory := range filepath.SplitList(toolPath) {
		candidate := filepath.Join(directory, name)
		info, err := os.Lstat(candidate)
		if err != nil || (info.Mode()&os.ModeSymlink == 0 && !info.Mode().IsRegular()) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !safeLinuxToolRoot(resolved) {
			continue
		}
		resolvedInfo, err := os.Stat(resolved)
		if err != nil || !resolvedInfo.Mode().IsRegular() || resolvedInfo.Mode().Perm()&0o111 == 0 || resolvedInfo.Mode().Perm()&0o022 != 0 {
			continue
		}
		return resolved, true
	}
	return "", false
}

func safeLinuxToolRoot(path string) bool {
	for _, root := range []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin", openCodeManagedToolRoot} {
		if pathInside(root, path) {
			return true
		}
	}
	return false
}

func safeLinuxToolVersion(ctx context.Context, path string, args []string, toolPath string) string {
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	capture := newBoundedCapture(4096)
	command := exec.CommandContext(versionCtx, path, args...)
	command.Env = []string{"PATH=" + toolPath, "HOME=/nonexistent", "LANG=C", "LC_ALL=C"}
	command.Stdout = capture
	command.Stderr = capture
	_ = command.Run()
	if versionCtx.Err() != nil || capture.Truncated() {
		return "unknown"
	}
	match := safeToolVersionPattern.FindString(capture.String())
	if match == "" {
		return "unknown"
	}
	if len(match) > 64 {
		return "unknown"
	}
	return strings.TrimPrefix(strings.ToLower(match), "v")
}

func ValidateLinuxToolInventory(entries []LinuxToolInventoryEntry) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.Capability == "" || entry.Version == "" || strings.ContainsAny(entry.Version, `/\\`) {
			return errors.New("linux tool inventory contains unsafe data")
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return errors.New("linux tool inventory contains duplicate tools")
		}
		seen[entry.Name] = struct{}{}
	}
	return nil
}
