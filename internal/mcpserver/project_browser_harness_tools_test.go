package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type browserHarnessToolStore struct {
	kind    edge.OperationKind
	request edge.OperationRequest
}

func (*browserHarnessToolStore) DeviceActive(string) bool { return true }
func (*browserHarnessToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}
func (s *browserHarnessToolStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	s.kind = kind
	s.request = request
	return edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}, true, nil
}
func (*browserHarnessToolStore) OperationStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserHarnessToolStore) ActiveOperations(string, int) ([]edge.Operation, error) {
	return nil, nil
}
func (*browserHarnessToolStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserHarnessToolStore) RequestOperationCancel(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*browserHarnessToolStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}
func (s *browserHarnessToolStore) WaitOperation(_ context.Context, id string, _ time.Duration) (edge.Operation, error) {
	return edge.Operation{ID: id, Kind: s.kind, State: edge.OperationSucceeded, Result: edge.OperationResult{WorkspaceID: "ws_33333333333333333333333333333333", ProjectAlias: "mcp-devbox", ProjectOwner: "charle-z", ProjectRepository: "mcp-devbox", ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev", BrowserHarnessRunID: "bh_44444444444444444444444444444444", BrowserHarnessState: "running", BrowserHarnessProfile: "default", BrowserHarnessCreatedAt: "2026-08-04T04:00:00Z", BrowserHarnessUpdatedAt: "2026-08-04T04:00:01Z", BrowserHarnessTimeoutSeconds: 3600, BrowserHarnessStorageMiB: 2048, BrowserHarnessStdoutEOF: true, BrowserHarnessStderrEOF: true}}, nil
}

func TestProjectBrowserHarnessToolsExposeArbitraryToolboxExecution(t *testing.T) {
	store := &browserHarnessToolStore{}
	server := New(nil).WithEdgeStore(store)
	for _, name := range []string{"project_browser_harness_start", "project_browser_harness_status", "project_browser_harness_list", "project_browser_harness_stop", "project_browser_harness_cleanup", "project_browser_harness_artifact_list", "project_browser_harness_artifact_read"} {
		if _, ok := server.table[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	entry := server.table["project_browser_harness_start"]
	if entry.def.Annotations["destructiveHint"] != true || entry.def.Annotations["idempotentHint"] != true || entry.def.Annotations["openWorldHint"] != true {
		t.Fatalf("annotations=%v", entry.def.Annotations)
	}
	encoded, _ := json.Marshal(entry.def.InputSchema)
	if strings.Contains(string(encoded), `"action"`) || strings.Contains(string(encoded), `"selector"`) || !strings.Contains(string(encoded), `"argv"`) {
		t.Fatalf("schema=%s", encoded)
	}
	output, err := entry.handler(json.RawMessage(`{"alias":"mcp-devbox","target":"parrot-trusted-linux","idempotency_key":"harness-start-1","profile":"default","argv":["node","tests/e2e.mjs"],"cwd":"tests","environment":{"CI":"true"},"timeout_seconds":3600,"storage_mib":2048}`))
	if err != nil || store.kind != edge.OperationProjectBrowserHarnessStart || len(store.request.Argv) != 2 || store.request.BrowserHarnessProfile != "default" || !strings.Contains(output, `"run_id":"bh_44444444444444444444444444444444"`) {
		t.Fatalf("kind=%s request=%+v output=%s err=%v", store.kind, store.request, output, err)
	}
}
