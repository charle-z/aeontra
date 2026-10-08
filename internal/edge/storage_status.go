package edge

import "regexp"

var storageDriverPattern = regexp.MustCompile(`^(?:unknown|unavailable|[a-z0-9][a-z0-9._-]{0,31})$`)

func hasEdgeStorageResult(result OperationResult) bool {
	return result.StorageTotalBytes != 0 ||
		result.StorageAvailableBytes != 0 ||
		result.StorageReservedMinBytes != 0 ||
		result.StoragePressure != "" ||
		result.StorageDriver != "" ||
		result.StorageDriverPosture != ""
}

func validEdgeStorageResult(result OperationResult) bool {
	if result.StorageTotalBytes == 0 ||
		result.StorageAvailableBytes > result.StorageTotalBytes ||
		!storageDriverPattern.MatchString(result.StorageDriver) {
		return false
	}

	switch result.StoragePressure {
	case "unconfigured":
		if result.StorageReservedMinBytes != 0 {
			return false
		}
	case "normal":
		if result.StorageReservedMinBytes == 0 || result.StorageAvailableBytes < result.StorageReservedMinBytes {
			return false
		}
	case "critical":
		if result.StorageReservedMinBytes == 0 || result.StorageAvailableBytes >= result.StorageReservedMinBytes {
			return false
		}
	default:
		return false
	}

	switch result.StorageDriverPosture {
	case "copy_on_write":
		switch result.StorageDriver {
		case "overlay", "overlay2", "fuse-overlayfs", "btrfs", "zfs":
		default:
			return false
		}
	case "degraded":
		if result.StorageDriver != "vfs" {
			return false
		}
	case "unknown":
		switch result.StorageDriver {
		case "overlay", "overlay2", "fuse-overlayfs", "btrfs", "zfs", "vfs":
			return false
		}
	default:
		return false
	}

	base := result
	base.StorageTotalBytes = 0
	base.StorageAvailableBytes = 0
	base.StorageReservedMinBytes = 0
	base.StoragePressure = ""
	base.StorageDriver = ""
	base.StorageDriverPosture = ""
	return validOperationCompletion(base, "")
}
