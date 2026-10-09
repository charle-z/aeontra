package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectProcessToolStore struct {
	created   []edge.Operation
	processes []edge.BackgroundProcessSummary
}

func (*projectProcessToolStore) DeviceActive(string) bool { return true }
func (*projectProcessToolStore) ResolveActiveDeviceName(name string) (edge.Device, error) {
	return edge.Device{ID: "ed_11111111111111111111111111111111", Name: name, State: edge.StateActive}, nil
}
func (store *projectProcessToolStore) CreateOperation(deviceID string, kind edge.OperationKind, request edge.OperationRequest) (edge.Operation, bool, error) {
	op := edge.Operation{ID: "eo_22222222222222222222222222222222", DeviceID: deviceID, Kind: kind, Request: request, State: edge.OperationQueued}
	store.created = append(store.created, op)
	return op, true, nil
}
func (*projectProcessToolStore) OperationStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectProcessToolStore) ActiveOperations(string, int) ([]edge.Operation, error) {
	return nil, nil
}
func (*projectProcessToolStore) OperationLifecycleStatus(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectProcessToolStore) RequestOperationCancel(string) (edge.Operation, error) {
	return edge.Operation{}, nil
}
func (*projectProcessToolStore) AutopilotStatus(string) (edge.OperationResult, error) {
	return edge.OperationResult{}, nil
}
func (store *projectProcessToolStore) WaitOperation(_ context.Context, operationID string, _ time.Duration) (edge.Operation, error) {
	created := store.created[len(store.created)-1]
	result := edge.OperationResult{
		WorkspaceID: "ws_33333333333333333333333333333333", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		BackgroundProcessID: "pr_44444444444444444444444444444444", BackgroundProcessState: "running", BackgroundStartedAt: "2026-08-02T14:00:00Z",
		BackgroundStdout: "ready\n", BackgroundStdoutNext: 6,
		BackgroundProcesses: store.processes,
	}
	if created.Kind == edge.OperationProjectProcessStdin {
		result.BackgroundStdout = ""
		result.BackgroundStdoutNext = 0
		result.BackgroundStdinNext = int64(len(created.Request.Stdin))
		result.BackgroundStdinAccepted = len(created.Request.Stdin)
	}
	return edge.Operation{ID: operationID, Kind: created.Kind, State: edge.OperationSucceeded, Result: result}, nil
}

func TestProjectProcessToolsQueueClosedOperationsWithoutLeaks(t *testing.T) {
	store := &projectProcessToolStore{}
	server := New(nil).WithEdgeStore(store)
	cases := []struct {
		name string
		body string
		kind edge.OperationKind
	}{
		{"project_process_start", `{"alias":"project","target":"parrot","idempotency_key":"process-1","argv":["go","run","."],"cwd":"cmd","stdin":"ready\n","environment":{"PORT":"8080"}}`, edge.OperationProjectProcessStart},
		{"project_process_status", `{"alias":"project","target":"parrot","process_id":"pr_44444444444444444444444444444444","stdout_offset":0,"stderr_offset":0,"limit_bytes":4096}`, edge.OperationProjectProcessStatus},
		{"project_process_stdin", `{"alias":"project","target":"parrot","process_id":"pr_44444444444444444444444444444444","idempotency_key":"stdin-1","expected_offset":0,"data":"{\"jsonrpc\":\"2.0\"}\n","close_stdin":false}`, edge.OperationProjectProcessStdin},
		{"project_process_stop", `{"alias":"project","target":"parrot","process_id":"pr_44444444444444444444444444444444","grace_seconds":5}`, edge.OperationProjectProcessStop},
		{"project_process_signal", `{"alias":"project","target":"parrot","process_id":"pr_44444444444444444444444444444444","signal":"interrupt"}`, edge.OperationProjectProcessSignal},
		{"project_process_list", `{"alias":"project","target":"parrot","limit":20}`, edge.OperationProjectProcessList},
		{"project_process_cleanup", `{"alias":"project","target":"parrot","process_id":"pr_44444444444444444444444444444444"}`, edge.OperationProjectProcessCleanup},
	}
	for _, test := range cases {
		entry, ok := server.table[test.name]
		if !ok {
			t.Fatalf("missing %s", test.name)
		}
		output, err := entry.handler(json.RawMessage(test.body))
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if store.created[len(store.created)-1].Kind != test.kind || (test.kind != edge.OperationProjectProcessList && !strings.Contains(output, `"process_id":"pr_44444444444444444444444444444444"`)) {
			t.Fatalf("%s output=%s created=%+v", test.name, output, store.created)
		}
		for _, forbidden := range []string{"device_id", "workspace_id", `"argv"`, `"environment"`, `"pid"`, "/home/", ".local/state"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("%s exposed %q: %s", test.name, forbidden, output)
			}
		}
	}
}

func TestProjectProcessListReturnsEmptyArrayWithoutSingleProcessFields(t *testing.T) {
	store := &projectProcessToolStore{}
	server := New(nil).WithEdgeStore(store)
	body, err := server.handleProjectProcessList(json.RawMessage(`{"alias":"project","target":"parrot","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	processes, ok := view["processes"].([]any)
	if !ok || len(processes) != 0 {
		t.Fatalf("empty list must be an array: %s", body)
	}
	for _, key := range []string{"process_id", "process_state", "exit_known", "exit_code", "stdin_next", "stdout", "stderr"} {
		if _, found := view[key]; found {
			t.Fatalf("list exposed single-process field %s: %s", key, body)
		}
	}
}

func TestProjectProcessListPreservesEachProcessExitEvidence(t *testing.T) {
	store := &projectProcessToolStore{processes: []edge.BackgroundProcessSummary{{ProcessID: "pr_44444444444444444444444444444444", State: "exited", ExitKnown: true, ExitCode: 7}}}
	server := New(nil).WithEdgeStore(store)
	body, err := server.handleProjectProcessList(json.RawMessage(`{"alias":"project","target":"parrot","limit":3}`))
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Processes []edge.BackgroundProcessSummary `json:"processes"`
	}
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Processes) != 1 || !view.Processes[0].ExitKnown || view.Processes[0].ExitCode != 7 {
		t.Fatalf("exit evidence lost: %s", body)
	}
}
