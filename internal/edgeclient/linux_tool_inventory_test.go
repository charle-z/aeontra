package edgeclient

import (
	"context"
	"strings"
	"testing"
)

func TestCollectLinuxToolInventoryReturnsOnlySanitizedMetadata(t *testing.T) {
	entries, err := CollectLinuxToolInventory(context.Background(), openCodeDefaultToolPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 20 {
		t.Fatalf("inventory too small: %d", len(entries))
	}
	if err := ValidateLinuxToolInventory(entries); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.Name] = true
		if strings.ContainsAny(entry.Version, `/\\`) || strings.Contains(entry.Version, " ") || len(entry.Version) > 64 {
			t.Fatalf("unsafe version for %s: %q", entry.Name, entry.Version)
		}
		if entry.Available && entry.Version == "absent" {
			t.Fatalf("available tool reported absent version: %+v", entry)
		}
		if !entry.Available && entry.Version != "absent" {
			t.Fatalf("missing tool reported non-absent version: %+v", entry)
		}
	}
	for _, required := range []string{"nmap", "ssh", "python", "go", "node", "docker", "podman"} {
		if !seen[required] {
			t.Fatalf("inventory missing %q", required)
		}
	}
}

func TestValidateLinuxToolInventoryRejectsPathsAndDuplicates(t *testing.T) {
	invalid := [][]LinuxToolInventoryEntry{
		{{Name: "python", Available: true, Version: "/usr/bin/python3", Capability: "python-runtime"}},
		{{Name: "python", Available: true, Version: "3.12.1", Capability: "python-runtime"}, {Name: "python", Version: "absent", Capability: "python-runtime"}},
		{{Name: "", Version: "absent", Capability: "python-runtime"}},
	}
	for _, entries := range invalid {
		if err := ValidateLinuxToolInventory(entries); err == nil {
			t.Fatalf("invalid inventory accepted: %+v", entries)
		}
	}
}

func TestFindSafeLinuxToolRejectsPathNames(t *testing.T) {
	for _, name := range []string{"../bin/sh", "/bin/sh", `bin\\sh`} {
		if path, ok := findSafeLinuxTool(name, openCodeDefaultToolPath); ok || path != "" {
			t.Fatalf("unsafe tool name accepted: %q -> %q", name, path)
		}
	}
}
