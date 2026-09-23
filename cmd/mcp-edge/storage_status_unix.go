//go:build !windows

package main

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const edgeStorageReservedMinBytesEnv = "MCP_DEVBOX_STORAGE_RESERVED_MIN_BYTES"

var (
	edgeStorageStatfs           = syscall.Statfs
	edgeStorageDiscoverRootless = edgeclient.DiscoverRootlessContainerEndpoint
	edgeStorageDriver           = edgeclient.RootlessContainerStorageDriver
)

func collectEdgeStorageStatus(stateRoot string) (edge.OperationResult, string) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	info, err := os.Lstat(stateRoot)
	if err != nil || !filepath.IsAbs(stateRoot) || stateRoot == string(filepath.Separator) ||
		!info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(info) {
		return edge.OperationResult{}, "storage_status_unavailable"
	}

	var stat syscall.Statfs_t
	if edgeStorageStatfs(stateRoot, &stat) != nil || stat.Bsize <= 0 {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	blockSize := uint64(stat.Bsize)
	if stat.Blocks == 0 || stat.Blocks > math.MaxUint64/blockSize || stat.Bavail > math.MaxUint64/blockSize {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	totalBytes := stat.Blocks * blockSize
	availableBytes := stat.Bavail * blockSize
	if availableBytes > totalBytes {
		return edge.OperationResult{}, "storage_status_unavailable"
	}

	result := edge.OperationResult{
		StorageTotalBytes:     totalBytes,
		StorageAvailableBytes: availableBytes,
		StoragePressure:       "unconfigured",
		StorageDriver:         "unavailable",
		StorageDriverPosture:  "unknown",
	}

	if raw := strings.TrimSpace(os.Getenv(edgeStorageReservedMinBytesEnv)); raw != "" {
		reserved, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || reserved == 0 {
			return edge.OperationResult{}, "storage_policy_invalid"
		}
		result.StorageReservedMinBytes = reserved
		result.StoragePressure = "normal"
		if availableBytes < reserved {
			result.StoragePressure = "critical"
		}
	}

	endpoint, discoverErr := edgeStorageDiscoverRootless(os.Geteuid(), "")
	if discoverErr == nil && endpoint != nil {
		driver, driverErr := edgeStorageDriver(context.Background(), endpoint, "")
		if driverErr == nil {
			result.StorageDriver = driver
			result.StorageDriverPosture = edgeclient.RootlessContainerStoragePosture(driver)
		} else {
			result.StorageDriver = "unknown"
		}
	}

	return result, ""
}

func mergeEdgeStorageStatus(base, storage edge.OperationResult) edge.OperationResult {
	base.StorageTotalBytes = storage.StorageTotalBytes
	base.StorageAvailableBytes = storage.StorageAvailableBytes
	base.StorageReservedMinBytes = storage.StorageReservedMinBytes
	base.StoragePressure = storage.StoragePressure
	base.StorageDriver = storage.StorageDriver
	base.StorageDriverPosture = storage.StorageDriverPosture
	return base
}

func collectEdgeOnboardingStatus(stateRoot string) (edge.OperationResult, string) {
	base, code := collectEdgeDiagnostic(stateRoot, true)
	if code != "" {
		return edge.OperationResult{}, code
	}
	storage, code := collectEdgeStorageStatus(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	return mergeEdgeStorageStatus(base, storage), ""
}
