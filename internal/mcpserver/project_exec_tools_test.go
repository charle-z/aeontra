package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectExecToolStore struct {
	resolvedTarget string
	createdKind    edge.OperationKind
	createdRequest edge.OperationRequest
}

func (*projectExecToolStore) DeviceActive(string) bool { return true }

func (store *projectExecToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	store.resolvedTarget = name
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}

func (store *projectExecToolStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	store.createdKind = kind
	store.createdRequest = request
	return edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}, true, nil
}

func (*projectExecToolStore) OperationStatus(string) (edge.Operation, error) { return edge.Operation{}, nil }
func (*projectExecToolStore) ActiveOperations(string, int) ([]edge.Operation, error) { return nil, nil }
func (*projectExecToolStore) OperationLifecycleStatus(string) (edge.Operation, error) { return edge.Operation{}, nil }
func (*projectExecToolStore) RequestOperationCancel(string) (edge.Operation, error) { return edge.Operation{}, nil }
func (*projectExecToolStore) AutopilotStatus(string) (edge.OperationResult, error) { return edge.OperationResult{}, nil }

func (store *projectExecToolStore) WaitOperation(_ context.Context, operationID string, _ time.Duration) (edge.Operation, error) {
	return edge.Operation{
		ID: operationID, DeviceID: "ed_11111111111111111111111111111111", Kind: store.createdKind,
		State: edge.OperationSucceeded,
		Result: edge.OperationResult{
			WorkspaceID: "ws_33333333333333333333333333333333",
			ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
			ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
			ExecCompleted: true, ExecExitCode: 0, ExecStdout: "ok\n",
		},
	}, nil
}

func TestProjectExecQueuesOneBoundedWorkcellCommand(t *testing.T) {
	store := &projectExecToolStore{}
	server := New(nil).WithEdgeStore(store)
	entry, ok := server.table["project_exec"]
	if !ok {
		t.Fatal("missing project_exec")
	}
	annotations := entry.def.Annotations
	if annotations["readOnlyHint"] != false || annotations["destructiveHint"] != true || annotations["idempotentHint"] != true || annotations["openWorldHint"] != true {
		t.Fatalf("annotations=%+v", annotations)
	}
	output, err := entry.handler(json.RawMessage(`{"alias":"project","target":"parrot","idempotency_key":"chat-exec-1","argv":["go","test","./..."],"cwd":"internal","stdin":"input\n","environment":{"CI":"true"},"timeout_seconds":90}`))
	if err != nil {
		t.Fatal(err)
	}
	if store.resolvedTarget != "parrot" || store.createdKind != edge.OperationProjectExec {
		t.Fatalf("target=%q kind=%q", store.resolvedTarget, store.createdKind)
	}
	expected := edge.OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "chat-exec-1",
		Argv: []string{"go", "test", "./..."}, CWD: "internal", Stdin: "input\n",
		Environment: map[string]string{"CI": "true"}, TimeoutSeconds: 90,
	}
	if !reflect.DeepEqual(store.createdRequest, expected) {
		t.Fatalf("request=%+v", store.createdRequest)
	}
	for _, required := range []string{
		`"operation_id":"eo_22222222222222222222222222222222"`, `"state":"succeeded"`,
		`"alias":"project"`, `"repository":"charle-z/repo"`, `"target":"parrot"`,
		`"exit_code":0`, `"stdout":"ok\n"`, `"timed_out":false`,
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("execution output missing %q: %s", required, output)
		}
	}
	for _, forbidden := range []string{"device_id", "workspace_id", "ed_111", "ws_333", `"environment"`, `"argv"`} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("execution output exposed %q: %s", forbidden, output)
		}
	}
}
