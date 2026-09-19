package edge

import "testing"

func TestStorageStatusResultValidation(t *testing.T) {
	result := OperationResult{
		StorageTotalBytes:           1000,
		StorageFreeBytes:            400,
		StorageReservedMinBytes:     300,
		StoragePressureState:        "normal",
		StorageDriver:               "overlay",
		StorageDriverPosture:        "normal",
		StorageManagedBytes:         75,
		StorageManagedBytesComplete: true,
		StorageCategories: []StorageCategorySummary{{
			Class:            "package_caches",
			Bytes:            75,
			ObjectCount:      1,
			ReclaimableBytes: 0,
			Complete:         true,
		}},
	}
	if !validOperationCompletionForKind(OperationStorageStatus, result, "") {
		t.Fatal("valid storage status rejected")
	}

	badReclaimable := result
	badReclaimable.StorageCategories = append([]StorageCategorySummary(nil), result.StorageCategories...)
	badReclaimable.StorageCategories[0].ReclaimableBytes = 76
	if validOperationCompletionForKind(OperationStorageStatus, badReclaimable, "") {
		t.Fatal("reclaimable bytes above measured bytes accepted")
	}

	badPressure := result
	badPressure.StorageReservedMinBytes = 500
	if validOperationCompletionForKind(OperationStorageStatus, badPressure, "") {
		t.Fatal("inconsistent storage pressure accepted")
	}

	unconfigured := result
	unconfigured.StorageReservedMinBytes = 0
	unconfigured.StoragePressureState = "unconfigured"
	if !validOperationCompletionForKind(OperationStorageStatus, unconfigured, "") {
		t.Fatal("unconfigured reserve posture rejected")
	}
}

func TestStorageStatusRejectsDuplicateOrUnknownCategories(t *testing.T) {
	base := OperationResult{
		StorageTotalBytes:        1000,
		StorageFreeBytes:         900,
		StoragePressureState:     "unconfigured",
		StorageDriver:            "unknown",
		StorageDriverPosture:     "unknown",
		StorageManagedBytes:      10,
		StorageCategories: []StorageCategorySummary{{
			Class:       "project_workspaces",
			Bytes:       10,
			ObjectCount: 1,
			Complete:    false,
		}},
	}
	duplicate := base
	duplicate.StorageCategories = append(append([]StorageCategorySummary(nil), base.StorageCategories...), base.StorageCategories[0])
	if validOperationCompletionForKind(OperationStorageStatus, duplicate, "") {
		t.Fatal("duplicate storage category accepted")
	}
	unknown := base
	unknown.StorageCategories = append([]StorageCategorySummary(nil), base.StorageCategories...)
	unknown.StorageCategories[0].Class = "whatever-directory"
	if validOperationCompletionForKind(OperationStorageStatus, unknown, "") {
		t.Fatal("unknown storage category accepted")
	}
}
