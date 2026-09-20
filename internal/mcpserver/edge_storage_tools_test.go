package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestEdgeStorageStatusToolReturnsOnlyBoundedPublicAccounting(t *testing.T) {
	store := &recordingEdgeControlStore{
		active: true,
		status: edge.OperationResult{
			StorageTotalBytes:           4096,
			StorageFreeBytes:            2048,
			StorageReservedMinBytes:     1024,
			StorageHighFreeBytes:        1536,
			StorageVFSMinFreeBytes:      3072,
			StoragePressureState:        "normal",
			StorageDriver:               "vfs",
			StorageDriverPosture:        "degraded",
			StorageManagedBytes:         512,
			StorageManagedBytesComplete: false,
			StorageCategories: []edge.StorageCategorySummary{{
				Class:            "project_workspaces",
				Bytes:            512,
				ObjectCount:      1,
				ReclaimableBytes: 0,
				Complete:         false,
			}},
		},
	}
	server := stampServer(t).WithEdgeStore(store)
	tool, ok := server.table["edge_storage_status"]
	if !ok {
		t.Fatal("edge_storage_status tool is not registered")
	}
	body, err := tool.handler(json.RawMessage("{\"device_id\":\"" + testEdgeDeviceID + "\"}"))
	if err != nil {
		t.Fatal(err)
	}
	if store.createdKind != edge.OperationStorageStatus {
		t.Fatalf("kind=%s", store.createdKind)
	}
	for _, want := range []string{
		"\"total_bytes\":4096",
		"\"free_bytes\":2048",
		"\"high_free_bytes\":1536",
		"\"vfs_min_free_bytes\":3072",
		"\"pressure_state\":\"normal\"",
		"\"driver\":\"vfs\"",
		"\"driver_posture\":\"degraded\"",
		"\"class\":\"project_workspaces\"",
		"\"reclaimable_bytes\":0",
		"\"complete\":false",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	for _, forbidden := range []string{"/home/", "/state/", "graphroot", "container_name", "socket_path"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("private storage detail leaked: %s", body)
		}
	}
}

func TestEdgeStorageGCToolsUseClosedRequestsAndBoundedPublicOutput(t *testing.T) {
	store := &recordingEdgeControlStore{
		active: true,
		status: edge.OperationResult{
			StorageGCPlanID:         "sgp_" + strings.Repeat("a", 32),
			StorageGCPlanExpiresAt:  "2099-01-01T00:00:00Z",
			StorageGCMinAgeSeconds:  3600,
			StorageGCCandidateCount: 2,
			StorageGCCandidateBytes: 0,
			StorageGCCategories: []edge.StorageCategorySummary{
				{Class: "browser_profiles_artifacts", ObjectCount: 1, Complete: true},
				{Class: "process_logs", ObjectCount: 1, Complete: true},
			},
		},
	}
	server := stampServer(t).WithEdgeStore(store)

	preview := server.table["edge_storage_gc_preview"]
	body, err := preview.handler(json.RawMessage("{\"device_id\":\"" + testEdgeDeviceID + "\",\"idempotency_key\":\"gc-preview-1\",\"min_age_seconds\":3600}"))
	if err != nil {
		t.Fatal(err)
	}
	if store.createdKind != edge.OperationStorageGCPreview || store.request.IdempotencyKey != "gc-preview-1" || store.request.StorageGCMinAgeSeconds != 3600 {
		t.Fatalf("preview kind=%s request=%+v", store.createdKind, store.request)
	}
	for _, want := range []string{
		"\"gc_plan_id\":\"sgp_" + strings.Repeat("a", 32) + "\"",
		"\"gc_min_age_seconds\":3600",
		"\"gc_candidate_count\":2",
		"\"class\":\"browser_profiles_artifacts\"",
		"\"class\":\"process_logs\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %s in %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"br_" + strings.Repeat("b", 32),
		"pr_" + strings.Repeat("c", 32),
		"/home/", "/state/", "container_name", "socket_path", "workspace_id",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("private gc detail leaked: %s", body)
		}
	}

	apply := server.table["edge_storage_gc_apply"]
	_, err = apply.handler(json.RawMessage("{\"device_id\":\"" + testEdgeDeviceID + "\",\"idempotency_key\":\"gc-apply-1\",\"plan_id\":\"sgp_" + strings.Repeat("a", 32) + "\"}"))
	if err != nil {
		t.Fatal(err)
	}
	if store.createdKind != edge.OperationStorageGCApply || store.request.IdempotencyKey != "gc-apply-1" || store.request.StorageGCPlanID != "sgp_"+strings.Repeat("a", 32) {
		t.Fatalf("apply kind=%s request=%+v", store.createdKind, store.request)
	}
}
