//go:build !windows

package main

import (
	"context"
	"errors"
	"syscall"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestCollectEdgeStorageStatusReportsCapacityDriverAndPressure(t *testing.T) {
	oldStatfs := edgeStorageStatfs
	oldDiscover := edgeStorageDiscoverRootless
	oldDriver := edgeStorageDriver
	t.Cleanup(func() {
		edgeStorageStatfs = oldStatfs
		edgeStorageDiscoverRootless = oldDiscover
		edgeStorageDriver = oldDriver
	})

	edgeStorageStatfs = func(_ string, stat *syscall.Statfs_t) error {
		stat.Blocks = 1000
		stat.Bavail = 250
		stat.Bsize = 4096
		return nil
	}
	edgeStorageDiscoverRootless = func(int, string) (*edgeclient.RootlessContainerEndpoint, error) {
		return &edgeclient.RootlessContainerEndpoint{
			Engine: "podman", SocketPath: "/run/user/1000/podman/podman.sock", Executable: "/usr/bin/podman",
		}, nil
	}
	edgeStorageDriver = func(context.Context, *edgeclient.RootlessContainerEndpoint, string) (string, error) {
		return "vfs", nil
	}

	t.Setenv(edgeStorageReservedMinBytesEnv, "")
	result, code := collectEdgeStorageStatus(t.TempDir())
	if code != "" {
		t.Fatalf("code=%q", code)
	}
	if result.StorageTotalBytes != 1000*4096 || result.StorageAvailableBytes != 250*4096 {
		t.Fatalf("storage bytes=%d/%d", result.StorageAvailableBytes, result.StorageTotalBytes)
	}
	if result.StorageReservedMinBytes != 0 || result.StoragePressure != "unconfigured" {
		t.Fatalf("unconfigured pressure=%+v", result)
	}
	if result.StorageDriver != "vfs" || result.StorageDriverPosture != "degraded" {
		t.Fatalf("driver posture=%q/%q", result.StorageDriver, result.StorageDriverPosture)
	}

	t.Setenv(edgeStorageReservedMinBytesEnv, "2048000")
	result, code = collectEdgeStorageStatus(t.TempDir())
	if code != "" {
		t.Fatalf("critical code=%q", code)
	}
	if result.StorageReservedMinBytes != 2048000 || result.StoragePressure != "critical" {
		t.Fatalf("critical pressure=%+v", result)
	}
}

func TestCollectEdgeStorageStatusFailsClosedOnInvalidPolicyAndDegradesDriverSafely(t *testing.T) {
	oldStatfs := edgeStorageStatfs
	oldDiscover := edgeStorageDiscoverRootless
	oldDriver := edgeStorageDriver
	t.Cleanup(func() {
		edgeStorageStatfs = oldStatfs
		edgeStorageDiscoverRootless = oldDiscover
		edgeStorageDriver = oldDriver
	})

	edgeStorageStatfs = func(_ string, stat *syscall.Statfs_t) error {
		stat.Blocks = 100
		stat.Bavail = 50
		stat.Bsize = 4096
		return nil
	}
	edgeStorageDiscoverRootless = func(int, string) (*edgeclient.RootlessContainerEndpoint, error) {
		return &edgeclient.RootlessContainerEndpoint{
			Engine: "podman", SocketPath: "/run/user/1000/podman/podman.sock", Executable: "/usr/bin/podman",
		}, nil
	}
	edgeStorageDriver = func(context.Context, *edgeclient.RootlessContainerEndpoint, string) (string, error) {
		return "", errors.New("unavailable")
	}

	t.Setenv(edgeStorageReservedMinBytesEnv, "not-a-number")
	if result, code := collectEdgeStorageStatus(t.TempDir()); code != "storage_policy_invalid" || result.StorageTotalBytes != 0 {
		t.Fatalf("invalid policy result=%+v code=%q", result, code)
	}

	t.Setenv(edgeStorageReservedMinBytesEnv, "")
	result, code := collectEdgeStorageStatus(t.TempDir())
	if code != "" || result.StorageDriver != "unknown" || result.StorageDriverPosture != "unknown" {
		t.Fatalf("driver fallback result=%+v code=%q", result, code)
	}
}
