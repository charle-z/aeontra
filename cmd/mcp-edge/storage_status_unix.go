//go:build !windows

package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const (
	edgeStorageMinFreeBytesEnv    = "MCP_DEVBOX_EDGE_STORAGE_MIN_FREE_BYTES"
	edgeStorageHighFreeBytesEnv   = "MCP_DEVBOX_EDGE_STORAGE_HIGH_FREE_BYTES"
	edgeStorageVFSMinFreeBytesEnv = "MCP_DEVBOX_EDGE_STORAGE_VFS_MIN_FREE_BYTES"
	maxStorageScanEntries         = 100000
	maxStorageWorkspaceRecords    = 4096
)

type storageWatermarks struct {
	Critical uint64
	High     uint64
	VFS      uint64
}

type storageTreeMeasurement struct {
	Bytes    uint64
	Entries  int
	Complete bool
}

func collectEdgeStorageStatus(ctx context.Context, stateRoot string) (edge.OperationResult, string) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(stateRoot, &stat); err != nil || stat.Bsize <= 0 {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	totalBytes, ok := storageBlockBytes(stat.Blocks, uint64(stat.Bsize))
	if !ok || totalBytes == 0 {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	freeBytes, ok := storageBlockBytes(stat.Bavail, uint64(stat.Bsize))
	if !ok || freeBytes > totalBytes {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	watermarks, code := configuredStorageWatermarks()
	if code != "" {
		return edge.OperationResult{}, code
	}

	registry, err := edgeclient.OpenWorkspaceRegistry(stateRoot)
	if err != nil {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	workspaces, err := registry.List()
	_ = registry.Close()
	if err != nil {
		return edge.OperationResult{}, "storage_status_unavailable"
	}
	roots, err := edgeclient.DefaultWorkspaceRoots()
	if err != nil {
		return edge.OperationResult{}, "storage_status_unavailable"
	}

	categories := initialStorageCategories()
	index := make(map[string]int, len(categories))
	for categoryIndex := range categories {
		index[categories[categoryIndex].Class] = categoryIndex
	}
	scanBudget := maxStorageScanEntries
	workspaceLimit := len(workspaces)
	if workspaceLimit > maxStorageWorkspaceRecords {
		workspaceLimit = maxStorageWorkspaceRecords
		categories[index["project_workspaces"]].Complete = false
		categories[index["project_runtime_state"]].Complete = false
		categories[index["package_caches"]].Complete = false
		categories[index["browser_profiles_artifacts"]].Complete = false
	}
	for _, workspace := range workspaces[:workspaceLimit] {
		workspaceCategory := &categories[index["project_workspaces"]]
		workspaceCategory.ObjectCount++
		if !storageWorkspacePathAllowed(workspace.Path, roots) {
			workspaceCategory.Complete = false
		} else if _, statErr := os.Lstat(workspace.Path); statErr != nil {
			workspaceCategory.Complete = false
		} else {
			addMeasuredStorageRoot(workspaceCategory, workspace.Path, &scanBudget, false)
		}
		addWorkspaceStateRoot(&categories[index["project_runtime_state"]], stateRoot, "project-runtime", workspace.ID, &scanBudget)
		addWorkspaceStateRoot(&categories[index["package_caches"]], stateRoot, "project-cache", workspace.ID, &scanBudget)
		addWorkspaceStateRoot(&categories[index["browser_profiles_artifacts"]], stateRoot, "project-artifacts", workspace.ID, &scanBudget)
	}
	addMeasuredStorageRoot(&categories[index["browser_profiles_artifacts"]], filepath.Join(stateRoot, "project-browser"), &scanBudget, true)
	addMeasuredStorageRoot(&categories[index["resultstore"]], filepath.Join(stateRoot, "results"), &scanBudget, true)
	addMeasuredStorageRoot(&categories[index["process_logs"]], filepath.Join(stateRoot, "project-process-logs"), &scanBudget, true)

	driver, posture := "unknown", "unknown"
	if endpoint, discoverErr := edgeclient.DiscoverRootlessContainerEndpoint(os.Geteuid(), ""); discoverErr == nil {
		if endpoint == nil {
			driver, posture = "none", "unavailable"
		} else {
			driverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			snapshot, inspectErr := edgeclient.InspectRootlessStorageDriver(driverCtx, endpoint)
			cancel()
			if inspectErr == nil {
				driver, posture = classifyStorageDriver(snapshot.Engine, snapshot.Driver, snapshot.Options)
			}
		}
	}

	var managedBytes uint64
	managedComplete := true
	for _, category := range categories {
		var sumOK bool
		managedBytes, sumOK = storageAdd(managedBytes, category.Bytes)
		if !sumOK {
			return edge.OperationResult{}, "storage_status_unavailable"
		}
		if !category.Complete {
			managedComplete = false
		}
	}
	return edge.OperationResult{
		StorageTotalBytes:           totalBytes,
		StorageFreeBytes:            freeBytes,
		StorageReservedMinBytes:     watermarks.Critical,
		StorageHighFreeBytes:        watermarks.High,
		StorageVFSMinFreeBytes:      watermarks.VFS,
		StoragePressureState:        storagePressureState(freeBytes, watermarks.Critical, watermarks.High),
		StorageDriver:               driver,
		StorageDriverPosture:        posture,
		StorageManagedBytes:         managedBytes,
		StorageManagedBytesComplete: managedComplete,
		StorageCategories:           categories,
	}, ""
}

func initialStorageCategories() []edge.StorageCategorySummary {
	complete := map[string]bool{
		"project_workspaces":         true,
		"project_runtime_state":      true,
		"package_caches":             true,
		"browser_profiles_artifacts": true,
		"resultstore":                true,
		"process_logs":               true,
	}
	classes := []string{
		"project_workspaces", "worker_worktrees", "persistent_toolboxes", "project_runtime_state",
		"shared_toolchains", "package_caches", "container_images_layers", "container_build_cache",
		"container_volumes", "browser_profiles_artifacts", "resultstore", "process_logs",
		"model_runtime_state", "temporary_downloads", "unique_evidence",
	}
	categories := make([]edge.StorageCategorySummary, 0, len(classes))
	for _, class := range classes {
		categories = append(categories, edge.StorageCategorySummary{Class: class, Complete: complete[class]})
	}
	return categories
}

func addWorkspaceStateRoot(category *edge.StorageCategorySummary, stateRoot, classRoot, workspaceID string, budget *int) {
	addMeasuredStorageRoot(category, filepath.Join(stateRoot, classRoot, workspaceID), budget, true)
}

func addMeasuredStorageRoot(category *edge.StorageCategorySummary, path string, budget *int, countIfExists bool) {
	if category == nil || budget == nil {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			category.Complete = false
		}
		return
	}
	if countIfExists {
		category.ObjectCount++
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || *budget <= 0 {
		category.Complete = false
		return
	}
	measured := measureStorageTree(path, *budget)
	*budget -= measured.Entries
	bytes, ok := storageAdd(category.Bytes, measured.Bytes)
	if !ok {
		category.Complete = false
		return
	}
	category.Bytes = bytes
	if !measured.Complete {
		category.Complete = false
	}
}

func measureStorageTree(root string, maxEntries int) storageTreeMeasurement {
	measured := storageTreeMeasurement{Complete: true}
	if maxEntries <= 0 {
		measured.Complete = false
		return measured
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if measured.Entries >= maxEntries {
			measured.Complete = false
			return fs.SkipAll
		}
		measured.Entries++
		if walkErr != nil {
			measured.Complete = false
			if path == root {
				return fs.SkipAll
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			measured.Complete = false
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			measured.Complete = false
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			measured.Complete = false
			return nil
		}
		bytes := storageAllocatedBytes(info)
		var ok bool
		measured.Bytes, ok = storageAdd(measured.Bytes, bytes)
		if !ok {
			measured.Complete = false
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		measured.Complete = false
	}
	return measured
}

func storageAllocatedBytes(info os.FileInfo) uint64 {
	if info == nil {
		return 0
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Blocks > 0 {
		if bytes, valid := storageBlockBytes(uint64(stat.Blocks), 512); valid {
			return bytes
		}
	}
	if info.Size() > 0 {
		return uint64(info.Size())
	}
	return 0
}

func configuredStorageWatermarks() (storageWatermarks, string) {
	critical, criticalSet, ok := parseOptionalPositiveBytes(edgeStorageMinFreeBytesEnv)
	if !ok {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	high, highSet, ok := parseOptionalPositiveBytes(edgeStorageHighFreeBytesEnv)
	if !ok {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	vfs, vfsSet, ok := parseOptionalPositiveBytes(edgeStorageVFSMinFreeBytesEnv)
	if !ok {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	if !criticalSet && (highSet || vfsSet) {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	if highSet && high <= critical {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	if vfsSet && vfs < critical {
		return storageWatermarks{}, "storage_status_config_invalid"
	}
	return storageWatermarks{Critical: critical, High: high, VFS: vfs}, ""
}

func parseOptionalPositiveBytes(name string) (uint64, bool, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, true, false
	}
	return value, true, true
}

func storagePressureState(freeBytes, criticalBytes, highBytes uint64) string {
	if criticalBytes == 0 {
		return "unconfigured"
	}
	if freeBytes < criticalBytes {
		return "critical"
	}
	if highBytes != 0 && freeBytes < highBytes {
		return "high"
	}
	return "normal"
}

func storageAdmissionDecision(kind edge.OperationKind, freeBytes uint64, watermarks storageWatermarks, driver string) string {
	if !storageAdmissionRequired(kind) {
		return ""
	}
	if watermarks.Critical != 0 && freeBytes < watermarks.Critical {
		return "storage_pressure_critical"
	}
	if watermarks.VFS != 0 && driver == "vfs" && storageAdmissionUsesVFSReserve(kind) && freeBytes < watermarks.VFS {
		return "storage_vfs_pressure_critical"
	}
	return ""
}

func storageAdmissionRequired(kind edge.OperationKind) bool {
	switch kind {
	case edge.OperationProjectWorktreeCreate,
		edge.OperationProjectToolboxCreate,
		edge.OperationProjectToolboxInstall,
		edge.OperationProjectToolboxServiceStart,
		edge.OperationProjectBrowserCreate,
		edge.OperationProjectBrowserRun,
		edge.OperationProjectBrowserHarnessStart,
		edge.OperationProjectProcessStart:
		return true
	default:
		return false
	}
}

func storageAdmissionUsesVFSReserve(kind edge.OperationKind) bool {
	switch kind {
	case edge.OperationProjectToolboxCreate,
		edge.OperationProjectToolboxInstall,
		edge.OperationProjectToolboxServiceStart,
		edge.OperationProjectBrowserHarnessStart:
		return true
	default:
		return false
	}
}

func enforceStorageAdmission(ctx context.Context, stateRoot string, kind edge.OperationKind) string {
	if !storageAdmissionRequired(kind) {
		return ""
	}
	watermarks, code := configuredStorageWatermarks()
	if code != "" {
		return "storage_admission_config_invalid"
	}
	if watermarks.Critical == 0 {
		return ""
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(stateRoot, &stat); err != nil || stat.Bsize <= 0 {
		return "storage_admission_unavailable"
	}
	freeBytes, ok := storageBlockBytes(stat.Bavail, uint64(stat.Bsize))
	if !ok {
		return "storage_admission_unavailable"
	}
	driver := "unknown"
	if watermarks.VFS != 0 && storageAdmissionUsesVFSReserve(kind) {
		endpoint, discoverErr := edgeclient.DiscoverRootlessContainerEndpoint(os.Geteuid(), "")
		if discoverErr != nil || endpoint == nil {
			return "storage_driver_unknown"
		}
		driverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		snapshot, inspectErr := edgeclient.InspectRootlessStorageDriver(driverCtx, endpoint)
		cancel()
		if inspectErr != nil {
			return "storage_driver_unknown"
		}
		driver, _ = classifyStorageDriver(snapshot.Engine, snapshot.Driver, snapshot.Options)
		if driver == "unknown" {
			return "storage_driver_unknown"
		}
	}
	return storageAdmissionDecision(kind, freeBytes, watermarks, driver)
}

func classifyStorageDriver(engine, driver, options string) (string, string) {
	engine = strings.ToLower(strings.TrimSpace(engine))
	driver = strings.ToLower(strings.TrimSpace(driver))
	options = strings.ToLower(options)
	switch driver {
	case "overlay":
		if engine == "podman" && strings.Contains(options, "fuse-overlayfs") {
			return "fuse-overlayfs", "normal"
		}
		return "overlay", "normal"
	case "overlay2":
		return "overlay2", "normal"
	case "vfs":
		return "vfs", "degraded"
	default:
		return "unknown", "unknown"
	}
}

func storageWorkspacePathAllowed(path string, roots edgeclient.WorkspaceRoots) bool {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return false
	}
	for _, root := range []string{roots.Dev, roots.HTBLinux, roots.WindowsDev} {
		root = filepath.Clean(root)
		if !filepath.IsAbs(root) || root == string(filepath.Separator) {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func storageAdd(left, right uint64) (uint64, bool) {
	if ^uint64(0)-left < right {
		return 0, false
	}
	return left + right, true
}

func storageBlockBytes(blocks, blockSize uint64) (uint64, bool) {
	if blockSize == 0 || blocks > ^uint64(0)/blockSize {
		return 0, false
	}
	return blocks * blockSize, true
}
