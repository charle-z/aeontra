//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
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
	if got := storagePressureState(200, 0, 0); got != "unconfigured" {
		t.Fatalf("unconfigured=%q", got)
	}
	if got := storagePressureState(200, 100, 0); got != "normal" {
		t.Fatalf("normal=%q", got)
	}
	if got := storagePressureState(99, 100, 0); got != "critical" {
		t.Fatalf("critical=%q", got)
	}
	if got := storagePressureState(150, 100, 200); got != "high" {
		t.Fatalf("high=%q", got)
	}
	if got := storagePressureState(250, 100, 200); got != "normal" {
		t.Fatalf("high-normal=%q", got)
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
	t.Setenv(edgeStorageHighFreeBytesEnv, "")
	t.Setenv(edgeStorageVFSMinFreeBytesEnv, "")
	if watermarks, code := configuredStorageWatermarks(); watermarks != (storageWatermarks{}) || code != "" {
		t.Fatalf("unset watermarks=%+v code=%q", watermarks, code)
	}
	t.Setenv(edgeStorageMinFreeBytesEnv, "1073741824")
	t.Setenv(edgeStorageHighFreeBytesEnv, "2147483648")
	t.Setenv(edgeStorageVFSMinFreeBytesEnv, "3221225472")
	if watermarks, code := configuredStorageWatermarks(); watermarks.Critical != 1073741824 || watermarks.High != 2147483648 || watermarks.VFS != 3221225472 || code != "" {
		t.Fatalf("configured watermarks=%+v code=%q", watermarks, code)
	}
	for _, value := range []string{"0", "-1", "not-a-number"} {
		t.Setenv(edgeStorageMinFreeBytesEnv, value)
		t.Setenv(edgeStorageHighFreeBytesEnv, "")
		t.Setenv(edgeStorageVFSMinFreeBytesEnv, "")
		if watermarks, code := configuredStorageWatermarks(); watermarks != (storageWatermarks{}) || code != "storage_status_config_invalid" {
			t.Fatalf("invalid %q watermarks=%+v code=%q", value, watermarks, code)
		}
	}
	t.Setenv(edgeStorageMinFreeBytesEnv, "100")
	t.Setenv(edgeStorageHighFreeBytesEnv, "100")
	if _, code := configuredStorageWatermarks(); code != "storage_status_config_invalid" {
		t.Fatalf("high<=critical code=%q", code)
	}
	t.Setenv(edgeStorageHighFreeBytesEnv, "200")
	t.Setenv(edgeStorageVFSMinFreeBytesEnv, "99")
	if _, code := configuredStorageWatermarks(); code != "storage_status_config_invalid" {
		t.Fatalf("vfs<critical code=%q", code)
	}
}

func TestStorageAdmissionBlocksOnlyNewHeavyAllocation(t *testing.T) {
	watermarks := storageWatermarks{Critical: 100, High: 200, VFS: 300}
	for _, kind := range []edge.OperationKind{
		edge.OperationProjectWorktreeCreate,
		edge.OperationProjectToolboxCreate,
		edge.OperationProjectToolboxInstall,
		edge.OperationProjectToolboxServiceStart,
		edge.OperationProjectBrowserCreate,
		edge.OperationProjectBrowserRun,
		edge.OperationProjectBrowserHarnessStart,
		edge.OperationProjectProcessStart,
	} {
		if code := storageAdmissionDecision(kind, 99, watermarks, "overlay"); code != "storage_pressure_critical" {
			t.Fatalf("%s critical code=%q", kind, code)
		}
		if code := storageAdmissionDecision(kind, 150, watermarks, "overlay"); code != "" {
			t.Fatalf("%s high must remain admitted, code=%q", kind, code)
		}
	}
	for _, kind := range []edge.OperationKind{
		edge.OperationStorageStatus,
		edge.OperationProjectWorktreeCleanup,
		edge.OperationProjectToolboxCleanup,
		edge.OperationProjectBrowserCleanup,
		edge.OperationProjectBrowserClose,
		edge.OperationProjectBrowserHarnessCleanup,
		edge.OperationProjectBrowserHarnessStop,
		edge.OperationProjectProcessCleanup,
		edge.OperationProjectProcessStop,
	} {
		if code := storageAdmissionDecision(kind, 1, watermarks, "vfs"); code != "" {
			t.Fatalf("%s recovery operation blocked: %q", kind, code)
		}
	}
}

func TestStorageAdmissionUsesStricterConfiguredVFSReserve(t *testing.T) {
	watermarks := storageWatermarks{Critical: 100, High: 200, VFS: 300}
	if code := storageAdmissionDecision(edge.OperationProjectToolboxCreate, 250, watermarks, "vfs"); code != "storage_vfs_pressure_critical" {
		t.Fatalf("vfs toolbox code=%q", code)
	}
	if code := storageAdmissionDecision(edge.OperationProjectBrowserHarnessStart, 250, watermarks, "vfs"); code != "storage_vfs_pressure_critical" {
		t.Fatalf("vfs harness code=%q", code)
	}
	if code := storageAdmissionDecision(edge.OperationProjectWorktreeCreate, 250, watermarks, "vfs"); code != "" {
		t.Fatalf("worktree incorrectly used vfs reserve: %q", code)
	}
}

func TestEnforceStorageAdmissionBlocksNewHeavyAllocationButNotCleanup(t *testing.T) {
	t.Setenv(edgeStorageMinFreeBytesEnv, "18446744073709551615")
	t.Setenv(edgeStorageHighFreeBytesEnv, "")
	t.Setenv(edgeStorageVFSMinFreeBytesEnv, "")
	root := t.TempDir()
	if code := enforceStorageAdmission(context.Background(), root, edge.OperationProjectWorktreeCreate); code != "storage_pressure_critical" {
		t.Fatalf("heavy allocation code=%q", code)
	}
	if code := enforceStorageAdmission(context.Background(), root, edge.OperationProjectWorktreeCleanup); code != "" {
		t.Fatalf("cleanup blocked code=%q", code)
	}
}
