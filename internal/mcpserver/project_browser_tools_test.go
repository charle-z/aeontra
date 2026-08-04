package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type browserToolStore struct {
	kind    edge.OperationKind
	request edge.OperationRequest
}

func (*browserToolStore) DeviceActive(string) bool { return true }
func (*browserToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}
func (s *browserToolStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	s.kind = kind
	s.request = request
	return edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}, true, nil
}
func (*browserToolStore) OperationStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserToolStore) ActiveOperations(string, int) ([]edge.Operation, error) { return nil, nil }
func (*browserToolStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserToolStore) RequestOperationCancel(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserToolStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}
func (s *browserToolStore) WaitOperation(_ context.Context, id string, _ time.Duration) (edge.Operation, error) {
	return edge.Operation{ID: id, Kind: s.kind, State: edge.OperationSucceeded, Result: edge.OperationResult{WorkspaceID: "ws_33333333333333333333333333333333", ProjectAlias: "mcp-devbox", ProjectOwner: "charle-z", ProjectRepository: "mcp-devbox", ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev", BrowserSessionID: "br_44444444444444444444444444444444", BrowserState: "ready", BrowserNetworkScope: "public", BrowserSafeURL: "https://example.com", BrowserRevision: 1, BrowserCreatedAt: "2026-08-04T02:00:00Z", BrowserUpdatedAt: "2026-08-04T02:00:01Z"}}, nil
}

func TestProjectBrowserToolsAreClosedAndDurable(t *testing.T) {
	store := &browserToolStore{}
	server := New(nil).WithEdgeStore(store)
	for _, name := range []string{"project_browser_create", "project_browser_status", "project_browser_list", "project_browser_run", "project_browser_artifact_read", "project_browser_close", "project_browser_cleanup"} {
		if _, ok := server.table[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	entry := server.table["project_browser_run"]
	if entry.def.Annotations["destructiveHint"] != true || entry.def.Annotations["idempotentHint"] != true || entry.def.Annotations["openWorldHint"] != true {
		t.Fatalf("annotations=%v", entry.def.Annotations)
	}
	output, err := entry.handler(json.RawMessage(`{"alias":"mcp-devbox","target":"parrot-trusted-linux","session_id":"br_44444444444444444444444444444444","idempotency_key":"run-1","timeout_seconds":60,"capture":"none","steps":[{"action":"navigate","url":"https://example.com"}]}`))
	if err != nil || store.kind != edge.OperationProjectBrowserRun || !strings.Contains(output, `"session_id":"br_44444444444444444444444444444444"`) {
		t.Fatalf("kind=%s output=%s err=%v", store.kind, output, err)
	}
}
