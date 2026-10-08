package edge

import "testing"

func validStorageDiagnosticFixture() OperationResult {
	return OperationResult{
		Release:               "p15.1.0",
		Commit:                "0123456789abcdef0123456789abcdef01234567",
		ManifestStatus:        "valid",
		ComponentsCompatible:  true,
		ProviderValid:         true,
		DriverValid:           true,
		StorageTotalBytes:     10 << 30,
		StorageAvailableBytes: 4 << 30,
		StoragePressure:       "unconfigured",
		StorageDriver:         "overlay",
		StorageDriverPosture:  "copy_on_write",
	}
}

func TestEdgeStorageResultIsBoundToOnboardingAndInternallyConsistent(t *testing.T) {
	result := validStorageDiagnosticFixture()
	if !validOperationCompletionForKind(OperationOnboardingStatus, result, "") {
		t.Fatal("valid onboarding storage result rejected")
	}
	if validOperationCompletionForKind(OperationBundleStatus, result, "") {
		t.Fatal("storage result escaped into bundle status")
	}

	reserved := result
	reserved.StorageReservedMinBytes = 5 << 30
	reserved.StoragePressure = "critical"
	if !validOperationCompletionForKind(OperationOnboardingStatus, reserved, "") {
		t.Fatal("critical pressure result rejected")
	}

	normal := result
	normal.StorageReservedMinBytes = 2 << 30
	normal.StoragePressure = "normal"
	if !validOperationCompletionForKind(OperationOnboardingStatus, normal, "") {
		t.Fatal("normal pressure result rejected")
	}
}

func TestEdgeStorageResultRejectsContradictionsAndPartialFailureMetadata(t *testing.T) {
	base := validStorageDiagnosticFixture()
	cases := []OperationResult{
		func() OperationResult {
			result := base
			result.StorageAvailableBytes = result.StorageTotalBytes + 1
			return result
		}(),
		func() OperationResult {
			result := base
			result.StorageReservedMinBytes = 5 << 30
			result.StoragePressure = "normal"
			return result
		}(),
		func() OperationResult {
			result := base
			result.StorageDriver = "vfs"
			result.StorageDriverPosture = "copy_on_write"
			return result
		}(),
		func() OperationResult {
			result := base
			result.StorageDriver = "overlay"
			result.StorageDriverPosture = "degraded"
			return result
		}(),
	}
	for index, result := range cases {
		if validOperationCompletionForKind(OperationOnboardingStatus, result, "") {
			t.Fatalf("invalid storage result %d accepted: %+v", index, result)
		}
	}
	if validOperationCompletionForKind(OperationOnboardingStatus, base, "storage_status_unavailable") {
		t.Fatal("failed onboarding accepted partial storage metadata")
	}
}
