package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type connectivityStore struct {
	recordingEdgeControlStore
	reads int
}

func (s *connectivityStore) DeviceConnectivity(string) (edge.DeviceConnectivity, error) {
	s.reads++
	return edge.DeviceConnectivity{State: "no_recent_contact", LastContactAt: "2026-10-09T17:00:00Z", ObservedAt: "2026-10-09T17:02:00Z", FreshnessSeconds: 90}, nil
}

func TestEdgeConnectivityStatusIsLocalReadOnlyAndAliasBound(t *testing.T) {
	store := &connectivityStore{recordingEdgeControlStore: recordingEdgeControlStore{active: true}}
	server := stampServer(t).WithEdgeStore(store)
	tool, ok := server.table["edge_connectivity_status"]
	if !ok {
		t.Fatal("connectivity tool missing")
	}
	for _, args := range []string{`{"target":"parrot"}`, `{"device_id":"` + testEdgeDeviceID + `"}`} {
		body, err := tool.handler(json.RawMessage(args))
		if err != nil || !strings.Contains(body, `"state":"no_recent_contact"`) || store.createdKind != "" {
			t.Fatalf("body=%s err=%v dispatched=%s", body, err, store.createdKind)
		}
		if strings.Contains(body, testEdgeDeviceID) {
			t.Fatal("device id leaked")
		}
	}
	for _, args := range []string{`{}`, `{"target":"unknown"}`, `{"target":"parrot","device_id":"` + testEdgeDeviceID + `"}`, `{"target":"parrot","force":true}`} {
		before := store.reads
		if _, err := tool.handler(json.RawMessage(args)); err == nil || before != store.reads {
			t.Fatalf("invalid args=%s err=%v", args, err)
		}
	}
}
