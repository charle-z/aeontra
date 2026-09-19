//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestMeasureStorageTreeIsBoundedAndDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "owned"), make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "large"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	measured := measureStorageTree(root, 16)
	if measured.Complete {
		t.Fatal("symlink ambiguity must make accounting incomplete")
	}
	if measured.Entries > 16 {
		t.Fatalf("entries=%d", measured.Entries)
	}
	if measured.Bytes >= 1<<20 {
		t.Fatalf("symlink target appears to have been counted: bytes=%d", measured.Bytes)
	}

	limited := measureStorageTree(root, 1)
	if limited.Complete || limited.Entries > 1 {
		t.Fatalf("bounded scan=%+v", limited)
	}
}

func TestStoragePressureAndDriverPostureAreClosed(t *testing.T) {
	if got := storagePressureState(200, 0); got != "unconfigured" {
		t.Fatalf("unconfigured=%q", got)
	}
	if got := storagePressureState(200, 100); got != "normal" {
		t.Fatalf("normal=%q", got)
	}
	if got := storagePressureState(99, 100); got != "critical" {
		t.Fatalf("critical=%q", got)
	}
	tests := []struct {
		engine, driver, options string
		wantDriver, wantPosture string
	}{
		{"podman", "overlay", "", "overlay", "normal"},
		{"podman", "overlay", "mount_program=/usr/bin/fuse-overlayfs", "fuse-overlayfs", "normal"},
		{"podman", "vfs", "", "vfs", "degraded"},
		{"docker", "overlay2", "", "overlay2", "normal"},
		{"podman", "mystery", "", "unknown", "unknown"},
	}
	for _, test := range tests {
		driver, posture := classifyStorageDriver(test.engine, test.driver, test.options)
		if driver != test.wantDriver || posture != test.wantPosture {
			t.Fatalf("%s/%s/%s => %s/%s", test.engine, test.driver, test.options, driver, posture)
		}
	}
}

func TestStorageCategoriesAreReadOnlyAndFailClosed(t *testing.T) {
	categories := initialStorageCategories()
	if len(categories) == 0 {
		t.Fatal("storage categories are empty")
	}
	incomplete := map[string]bool{}
	for _, category := range categories {
		if category.ReclaimableBytes != 0 {
			t.Fatalf("%s unexpectedly reclaimable: %d", category.Class, category.ReclaimableBytes)
		}
		if !category.Complete {
			incomplete[category.Class] = true
		}
	}
	for _, class := range []string{
		"worker_worktrees",
		"persistent_toolboxes",
		"container_images_layers",
		"container_build_cache",
		"container_volumes",
		"model_runtime_state",
		"unique_evidence",
	} {
		if !incomplete[class] {
			t.Fatalf("%s must remain incomplete until exact ownership accounting exists", class)
		}
	}
}

func TestStorageWorkspacePathAllowedRejectsOutsideOrRoot(t *testing.T) {
	roots := edgeclient.WorkspaceRoots{
		Dev:        "/srv/aeontra/dev",
		HTBLinux:   "/srv/aeontra/htb",
		WindowsDev: "/srv/aeontra/windows",
	}
	if !storageWorkspacePathAllowed("/srv/aeontra/dev/project-a", roots) {
		t.Fatal("owned workspace rejected")
	}
	for _, path := range []string{
		"/srv/aeontra/dev",
		"/srv/aeontra/dev/../outside",
		"/tmp/project-a",
		"/",
		"relative/project-a",
	} {
		if storageWorkspacePathAllowed(path, roots) {
			t.Fatalf("unsafe workspace path accepted: %s", path)
		}
	}
}

func TestConfiguredStorageReserveFailsClosed(t *testing.T) {
	t.Setenv(edgeStorageMinFreeBytesEnv, "")
	if reserve, code := configuredStorageReserve(); reserve != 0 || code != "" {
		t.Fatalf("unset reserve=%d code=%q", reserve, code)
	}
	t.Setenv(edgeStorageMinFreeBytesEnv, "1073741824")
	if reserve, code := configuredStorageReserve(); reserve != 1073741824 || code != "" {
		t.Fatalf("configured reserve=%d code=%q", reserve, code)
	}
	for _, value := range []string{"0", "-1", "not-a-number"} {
		t.Setenv(edgeStorageMinFreeBytesEnv, value)
		if reserve, code := configuredStorageReserve(); reserve != 0 || code != "storage_status_config_invalid" {
			t.Fatalf("invalid %q reserve=%d code=%q", value, reserve, code)
		}
	}
}
