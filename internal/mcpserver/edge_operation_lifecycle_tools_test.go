package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type lifecycleToolStore struct {
	operation edge.Operation
	target    string
	limit     int
}

func (store *lifecycleToolStore) DeviceActive(string) bool { return true }

func (store *lifecycleToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	store.target = name
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}

func (store *lifecycleToolStore) CreateOperation(string, edge.OperationKind, edge.OperationRequest) (edge.Operation, bool, error) {
	return edge.Operation{}, false, nil
}

func (store *lifecycleToolStore) OperationStatus(string) (edge.Operation, error) {
	return store.operation, nil
}

func (store *lifecycleToolStore) ActiveOperations(_ string, limit int) ([]edge.Operation, error) {
	store.limit = limit
	return []edge.Operation{store.operation}, nil
}

func (store *lifecycleToolStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return store.operation, nil
}

func (store *lifecycleToolStore) RequestOperationCancel(string) (edge.Operation, error) {
	store.operation.CancelRequested = true
	return store.operation, nil
}

func (store *lifecycleToolStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}

func (store *lifecycleToolStore) WaitOperation(context.Context, string, time.Duration) (edge.Operation, error) {
	return store.operation, nil
}

func TestEdgeOperationLifecycleToolsAreBoundedAndHideInternalState(t *testing.T) {
	created := time.Date(2026, 7, 29, 20, 50, 0, 0, time.UTC)
	store := &lifecycleToolStore{operation: edge.Operation{
		ID: "eo_22222222222222222222222222222222", DeviceID: "ed_11111111111111111111111111111111",
		Kind: edge.OperationProjectSnapshot, State: edge.OperationLeased,
		Request:   edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "private-key"},
		Result:    edge.OperationResult{WorkspaceID: "ws_33333333333333333333333333333333"},
		Progress:  edge.OperationProgress{Revision: 3, Phase: "running", CompletedUnits: 1, TotalUnits: 4},
		CreatedAt: created, LeasedAt: created.Add(100 * time.Millisecond), RunningAt: created.Add(150 * time.Millisecond),
		FinalizingAt: created.Add(700 * time.Millisecond), UpdatedAt: created.Add(800 * time.Millisecond),
	}}
	server := New(nil).WithEdgeStore(store)

	for _, name := range []string{"edge_operation_list", "edge_operation_status", "edge_operation_cancel"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		encoded, err := json.Marshal(entry.def.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"device_id", "workspace_id", "path", "request", "idempotency_key"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("%s schema exposed %q: %s", name, forbidden, encoded)
			}
		}
	}
	if server.table["edge_operation_list"].def.Annotations["readOnlyHint"] != true ||
		server.table["edge_operation_status"].def.Annotations["readOnlyHint"] != true ||
		server.table["edge_operation_cancel"].def.Annotations["destructiveHint"] != true {
		t.Fatal("operation lifecycle annotations are incorrect")
	}

	listed, err := server.table["edge_operation_list"].handler(json.RawMessage(`{"target":"parrot","limit":7}`))
	if err != nil || store.target != "parrot" || store.limit != 7 {
		t.Fatalf("listed=%s target=%q limit=%d err=%v", listed, store.target, store.limit, err)
	}
	status, err := server.table["edge_operation_status"].handler(json.RawMessage(`{"operation_id":"eo_22222222222222222222222222222222"}`))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := server.table["edge_operation_cancel"].handler(json.RawMessage(`{"operation_id":"eo_22222222222222222222222222222222"}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"list": listed, "status": status, "cancel": cancelled} {
		for _, required := range []string{`"operation_id":"eo_22222222222222222222222222222222"`, `"kind":"project_snapshot"`, `"phase":"running"`, `"project_alias":"project"`, `"target":"parrot"`} {
			if !strings.Contains(output, required) {
				t.Fatalf("%s output missing %q: %s", name, required, output)
			}
		}
		if !strings.Contains(output, "cancellable") {
			t.Fatalf("%s output omitted cancellation authority: %s", name, output)
		}
		for _, timing := range []string{`"queue_us":100000`, `"pickup_us":50000`, `"edge_work_us":550000`, `"completion_us":100000`, `"total_us":800000`} {
			if !strings.Contains(output, timing) {
				t.Fatalf("%s output missing timing %q: %s", name, timing, output)
			}
		}
		for _, forbidden := range []string{"device_id", "workspace_id", "ed_111", "ws_333", "private-key", "linux-workcell"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("%s output exposed %q: %s", name, forbidden, output)
			}
		}
	}
	if !strings.Contains(cancelled, `"cancel_requested":true`) {
		t.Fatalf("cancel output=%s", cancelled)
	}
}

func TestEdgeOperationListDefaultsAndRejectsUnsafeInput(t *testing.T) {
	store := &lifecycleToolStore{}
	server := New(nil).WithEdgeStore(store)
	if _, err := server.table["edge_operation_list"].handler(json.RawMessage(`{"target":"parrot"}`)); err != nil || store.limit != 20 {
		t.Fatalf("default limit=%d err=%v", store.limit, err)
	}
	for _, input := range []string{
		`{"target":"parrot","limit":51}`,
		`{"target":"parrot","device_id":"ed_11111111111111111111111111111111"}`,
		`{"operation_id":"bad"}`,
	} {
		name := "edge_operation_list"
		if strings.Contains(input, "operation_id") {
			name = "edge_operation_status"
		}
		if _, err := server.table[name].handler(json.RawMessage(input)); err == nil {
			t.Fatalf("%s accepted %s", name, input)
		}
	}
}

func TestPublicEdgeOperationExposesCurrentPhaseAndBundleIdentity(t *testing.T) {
	operation := edge.Operation{
		ID: "eo_33333333333333333333333333333333", Kind: edge.OperationEdgeRepair, State: edge.OperationLeased,
		Progress: edge.OperationProgress{Revision: 4, Phase: "repairing", CompletedUnits: 2, TotalUnits: 5},
		Result:   edge.OperationResult{EdgeProtocolVersion: "mcp-devbox.edge-bundle.v1", EdgeCatalogHash: "sha256:" + strings.Repeat("b", 64)},
	}
	body, err := json.Marshal(publicEdgeOperation(operation))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"progress_phase":"repairing"`, `"progress_completed_units":2`, `"progress_total_units":5`, `"edge_protocol_version":"mcp-devbox.edge-bundle.v1"`, `"edge_catalog_hash":"sha256:`} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("public view omitted %q: %s", expected, body)
		}
	}
}
