package edge

import (
	"testing"
	"time"
)

func TestStorageGCRequestValidation(t *testing.T) {
	preview := OperationRequest{IdempotencyKey: "storage-gc-preview-1", StorageGCMinAgeSeconds: 3600}
	if _, err := validateOperationRequest(OperationStorageGCPreview, preview); err != nil {
		t.Fatalf("valid preview rejected: %v", err)
	}
	for _, age := range []int{0, 59, 31536001} {
		bad := preview
		bad.StorageGCMinAgeSeconds = age
		if _, err := validateOperationRequest(OperationStorageGCPreview, bad); err == nil {
			t.Fatalf("preview age %d accepted", age)
		}
	}
	apply := OperationRequest{IdempotencyKey: "storage-gc-apply-1", StorageGCPlanID: "sgp_0123456789abcdef0123456789abcdef"}
	if _, err := validateOperationRequest(OperationStorageGCApply, apply); err != nil {
		t.Fatalf("valid apply rejected: %v", err)
	}
	badApply := apply
	badApply.StorageGCPlanID = "sgp_bad"
	if _, err := validateOperationRequest(OperationStorageGCApply, badApply); err == nil {
		t.Fatal("invalid plan id accepted")
	}
}

func TestStorageGCCompletionValidation(t *testing.T) {
	expires := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	preview := OperationResult{
		StorageGCPlanID:         "sgp_0123456789abcdef0123456789abcdef",
		StorageGCPlanExpiresAt:  expires,
		StorageGCMinAgeSeconds:  3600,
		StorageGCCandidateCount: 2,
		StorageGCCandidateBytes: 4096,
		StorageGCCategories: []StorageCategorySummary{
			{Class: "browser_profiles_artifacts", Bytes: 3072, ObjectCount: 1, Complete: true},
			{Class: "process_logs", Bytes: 1024, ObjectCount: 1, Complete: true},
		},
	}
	if !validOperationCompletionForKind(OperationStorageGCPreview, preview, "") {
		t.Fatal("valid gc preview rejected")
	}
	apply := preview
	apply.StorageGCRemovedCount = 2
	apply.StorageGCRemovedBytes = 4096
	if !validOperationCompletionForKind(OperationStorageGCApply, apply, "") {
		t.Fatal("valid gc apply rejected")
	}
	bad := preview
	bad.StorageGCCategories = append([]StorageCategorySummary(nil), preview.StorageGCCategories...)
	bad.StorageGCCategories[0].Class = "persistent_toolboxes"
	if validOperationCompletionForKind(OperationStorageGCPreview, bad, "") {
		t.Fatal("unsafe gc class accepted")
	}
}
