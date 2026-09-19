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
