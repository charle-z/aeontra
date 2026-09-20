package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestPublicEdgeOperationProjectsOnlySafeStorageMetadata(t *testing.T) {
	view := publicEdgeOperation(edge.Operation{
		ID:       "eo_0123456789abcdef0123456789abcdef",
		DeviceID: "ed_0123456789abcdef0123456789abcdef",
		State:    edge.OperationSucceeded,
		Result: edge.OperationResult{
			StorageTotalBytes:       100 << 30,
			StorageAvailableBytes:   40 << 30,
			StorageReservedMinBytes: 20 << 30,
			StoragePressure:         "normal",
			StorageDriver:           "overlay",
			StorageDriverPosture:    "copy_on_write",
		},
	})
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`"storage_total_bytes":107374182400`,
		`"storage_available_bytes":42949672960`,
		`"storage_reserved_min_bytes":21474836480`,
		`"storage_pressure":"normal"`,
		`"storage_driver":"overlay"`,
		`"storage_driver_posture":"copy_on_write"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("public storage view missing %s: %s", required, text)
		}
	}
	for _, forbidden := range []string{"/run/user", "/home/", "podman.sock", "docker.sock"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public storage view leaked %q: %s", forbidden, text)
		}
	}
}
