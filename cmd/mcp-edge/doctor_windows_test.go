//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWindowsDoctorServiceConfigRejectsUnknownFieldsAndOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install", "Aeontra", "Edge")
	state := filepath.Join(root, "state", "Aeontra", "State")
	workspace := filepath.Join(root, "workspace", "Aeontra", "Workspaces")
	if err := os.MkdirAll(installRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(installRoot, "service-config.json")
	valid := `{"version":1,"service":"AeontraEdge","service_identity":"NT SERVICE\\AeontraEdge","state_root":"` + filepath.ToSlash(state) + `","workspace_root":"` + filepath.ToSlash(workspace) + `"}`
	if err := os.WriteFile(file, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadWindowsDoctorServiceConfig(file, installRoot)
	if err != nil || config.StateRoot != state || config.WorkspaceRoot != workspace {
		t.Fatalf("valid config rejected: %#v %v", config, err)
	}
	if err := os.WriteFile(file, []byte(strings.TrimSuffix(valid, "}")+`,"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWindowsDoctorServiceConfig(file, installRoot); err == nil {
		t.Fatal("unknown service-config field accepted")
	}
	overlap := strings.Replace(valid, filepath.ToSlash(workspace), filepath.ToSlash(state), 1)
	if err := os.WriteFile(file, []byte(overlap), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWindowsDoctorServiceConfig(file, installRoot); err == nil {
		t.Fatal("overlapping workspace root accepted")
	}
	invalidLayout := strings.Replace(valid, filepath.ToSlash(state), filepath.ToSlash(filepath.Join(root, "state", "Aeontra", "Other")), 1)
	if err := os.WriteFile(file, []byte(invalidLayout), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWindowsDoctorServiceConfig(file, installRoot); err == nil {
		t.Fatal("unmanaged state layout accepted")
	}
}

func TestLoadWindowsDoctorServiceConfigAcceptsLegacyManagedState(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install", "Aeontra", "Edge")
	state := filepath.Join(root, "legacy", "Aeontra", "Edge")
	workspace := filepath.Join(root, "workspace", "Aeontra", "Workspaces")
	originalLegacyRoot := windowsDoctorLegacyStateRoot
	windowsDoctorLegacyStateRoot = func() (string, error) { return state, nil }
	t.Cleanup(func() { windowsDoctorLegacyStateRoot = originalLegacyRoot })
	for _, path := range []string{installRoot, state, workspace} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(installRoot, "service-config.json")
	content := `{"version":1,"service":"AeontraEdge","service_identity":"NT SERVICE\\AeontraEdge","state_root":"` + filepath.ToSlash(state) + `","workspace_root":"` + filepath.ToSlash(workspace) + `"}`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadWindowsDoctorServiceConfig(file, installRoot)
	if err != nil || config.StateRoot != state || config.WorkspaceRoot != workspace {
		t.Fatalf("legacy managed config rejected: %#v %v", config, err)
	}
}

func TestLoadWindowsDoctorActiveMarkerBindsLocalReleasePath(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "v1.2.3")
	if err := os.MkdirAll(filepath.Join(release, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "active.json")
	content := `{"version":1,"release":"v1.2.3","commit":"0123456789abcdef0123456789abcdef01234567","path":"` + filepath.ToSlash(release) + `"}`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	marker, err := loadWindowsDoctorActiveMarker(file, root)
	if err != nil || marker.Path != release {
		t.Fatalf("valid active marker rejected: %#v %v", marker, err)
	}
	if err := os.WriteFile(file, []byte(strings.Replace(content, `"path":"`+filepath.ToSlash(release)+`"`, `"path":"C:/Windows"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWindowsDoctorActiveMarker(file, root); err == nil {
		t.Fatal("active marker escaped install root")
	}
}
