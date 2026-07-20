package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type fixedWorkspaceEdgeStore struct {
	devices  map[string]bool
	bindings map[string]edge.WorkspaceBinding
}

func (store fixedWorkspaceEdgeStore) DeviceActive(deviceID string) bool {
	return store.devices[deviceID]
}

func (store fixedWorkspaceEdgeStore) ResolveWorkspace(workspaceID string) (edge.WorkspaceBinding, error) {
	binding, ok := store.bindings[workspaceID]
	if !ok {
		return edge.WorkspaceBinding{}, errors.New("registered active workspace not found")
	}
	return binding, nil
}

func continuationStore(workspaceID, profile, mode string) fixedWorkspaceEdgeStore {
	return fixedWorkspaceEdgeStore{
		devices: map[string]bool{testEdgeDeviceID: true},
		bindings: map[string]edge.WorkspaceBinding{
			workspaceID: {WorkspaceID: workspaceID, DeviceID: testEdgeDeviceID, Profile: profile, Mode: mode},
		},
	}
}

func continueCall(id any, workspaceID string, timeout int, extra string) string {
	encodedID, _ := json.Marshal(id)
	payload := map[string]any{"workspace_id": workspaceID, "timeout_seconds": timeout}
	if extra != "" {
		payload[extra] = "caller-controlled-sensitive-value"
	}
	encodedArguments, _ := json.Marshal(payload)
	return `{"jsonrpc":"2.0","id":` + string(encodedID) + `,"method":"tools/call","params":{"name":"workspace_runtime_continue","arguments":` + string(encodedArguments) + `}}`
}

func TestWorkspaceRuntimeContinueContractHasNoInstructionSurface(t *testing.T) {
	server := stampServer(t)
	entry, ok := server.table["workspace_runtime_continue"]
	if !ok || entry.requestHandler == nil {
		t.Fatal("workspace_runtime_continue is not registered as a request-bound tool")
	}
	if entry.def.Version != "1" || entry.def.InputSchema["additionalProperties"] != false {
		t.Fatalf("definition=%+v", entry.def)
	}
	wantHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	if !reflect.DeepEqual(entry.def.Annotations, wantHints) {
		t.Fatalf("annotations=%#v", entry.def.Annotations)
	}
	properties := entry.def.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"timeout_seconds", "workspace_id"}) {
		t.Fatalf("properties=%v", keys)
	}
	for _, forbidden := range []string{"objective", "prompt", "instructions", "command", "target", "host", "ip", "username", "password", "secret", "credential", "flag", "machine", "platform", "options"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("forbidden property %q", forbidden)
		}
	}
	for _, forbidden := range []string{"credential", "flag", "exploit", "target", "command"} {
		if strings.Contains(strings.ToLower(entry.def.Description), forbidden) {
			t.Fatalf("description contains %q: %s", forbidden, entry.def.Description)
		}
	}
}

func TestWorkspaceRuntimeContinueUsesFixedLocalContractGoalAndBoundedPublicView(t *testing.T) {
	server, store := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	text := toolText(t, call(t, server, continueCall("continue-1", testWorkspaceID, 300, "")))
	var view workspaceRuntimeContinueView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatal(err)
	}
	if view.RuntimeID == "" || view.WorkspaceID != testWorkspaceID || view.DeviceID != testEdgeDeviceID || view.State != modelturn.RuntimeStateAwaitingEdge || view.CreatedAt.IsZero() || view.ExpiresAt.IsZero() || view.LastSequence != 0 || view.FailureCategory != "" {
		t.Fatalf("view=%+v", view)
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &public); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(public))
	for key := range public {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"created_at", "device_id", "expires_at", "failure_category", "last_sequence", "runtime_id", "state", "workspace_id"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys=%v body=%s", keys, text)
	}
	for _, forbidden := range []string{"goal", "objective", "prompt", "instruction", "checkpoint", "command", "target", "credential", "flag", "profile", "mode"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("response contains %q: %s", forbidden, text)
		}
	}
	goal, _, err := store.RuntimeGoal(context.Background(), view.RuntimeID, testEdgeDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if string(goal) != workspaceContinuationGoal || strings.Contains(string(goal), testWorkspaceID) || strings.Contains(string(goal), "10.129") || strings.Contains(string(goal), "checkpoint-value") {
		t.Fatalf("goal=%q", goal)
	}
}

func TestWorkspaceRuntimeContinueAcceptsRegisteredHTBWorkspace(t *testing.T) {
	server, store := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "htb-linux"))
	text := toolText(t, call(t, server, continueCall("continue-htb-1", testWorkspaceID, 300, "")))
	var view workspaceRuntimeContinueView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatal(err)
	}
	if view.RuntimeID == "" || view.WorkspaceID != testWorkspaceID || view.DeviceID != testEdgeDeviceID || view.State != modelturn.RuntimeStateAwaitingEdge {
		t.Fatalf("view=%+v", view)
	}
	goal, _, err := store.RuntimeGoal(context.Background(), view.RuntimeID, testEdgeDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if string(goal) != workspaceContinuationGoal {
		t.Fatalf("goal=%q", goal)
	}
}

func TestWorkspaceRuntimeContinueIsReplaySafeAndCreatesLaterExplicitRuntime(t *testing.T) {
	server, store := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	first := toolText(t, call(t, server, continueCall("request-1", testWorkspaceID, 300, "")))
	replay := toolText(t, call(t, server, continueCall("request-1", testWorkspaceID, 300, "")))
	parallelExplicit := toolText(t, call(t, server, continueCall("request-2", testWorkspaceID, 300, "")))
	var firstView, replayView, parallelView workspaceRuntimeContinueView
	if err := json.Unmarshal([]byte(first), &firstView); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(replay), &replayView); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(parallelExplicit), &parallelView); err != nil {
		t.Fatal(err)
	}
	if firstView.RuntimeID != replayView.RuntimeID || firstView.RuntimeID != parallelView.RuntimeID {
		t.Fatalf("first=%s replay=%s parallel=%s", first, replay, parallelExplicit)
	}
	if err := store.CompleteRuntime(context.Background(), firstView.RuntimeID); err != nil {
		t.Fatal(err)
	}
	later := toolText(t, call(t, server, continueCall("request-3", testWorkspaceID, 300, "")))
	var laterView workspaceRuntimeContinueView
	if err := json.Unmarshal([]byte(later), &laterView); err != nil {
		t.Fatal(err)
	}
	if laterView.RuntimeID == firstView.RuntimeID {
		t.Fatalf("later explicit request reused terminal runtime: %s", later)
	}
	sameOldRequest := toolText(t, call(t, server, continueCall("request-1", testWorkspaceID, 300, "")))
	var oldReplay workspaceRuntimeContinueView
	if err := json.Unmarshal([]byte(sameOldRequest), &oldReplay); err != nil {
		t.Fatal(err)
	}
	if oldReplay.RuntimeID != firstView.RuntimeID {
		t.Fatalf("same request id was not idempotent: %s", sameOldRequest)
	}
}

func TestWorkspaceRuntimeContinueRejectsInjectionMissingBindingsAndInvalidModes(t *testing.T) {
	server, _ := modelTurnServer(t)
	if got := callToolErrorText(t, server, "workspace_runtime_continue", `{"workspace_id":"`+testWorkspaceID+`","timeout_seconds":300}`); got != errWorkspaceRegistryUnavailable.Error() {
		t.Fatalf("missing registry error=%q", got)
	}
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "htb-linux"))
	for _, forbidden := range []string{"objective", "prompt", "instructions", "command", "target", "host", "ip", "username", "password", "secret", "credential", "flag", "machine", "platform"} {
		response := call(t, server, continueCall("inject-"+forbidden, testWorkspaceID, 300, forbidden))
		var result toolResult
		encoded, _ := json.Marshal(response.Result)
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if !result.IsError || !strings.Contains(result.Content[0].Text, "unknown field") || strings.Contains(result.Content[0].Text, "caller-controlled-sensitive-value") {
			t.Fatalf("field=%s result=%s", forbidden, encoded)
		}
	}
	missingServer, _ := modelTurnServer(t)
	missingServer.WithEdgeStore(fixedWorkspaceEdgeStore{devices: map[string]bool{testEdgeDeviceID: true}, bindings: map[string]edge.WorkspaceBinding{}})
	if got := callToolErrorText(t, missingServer, "workspace_runtime_continue", `{"workspace_id":"`+testWorkspaceID+`","timeout_seconds":300}`); !strings.Contains(got, "not found") {
		t.Fatalf("missing workspace error=%q", got)
	}
	invalidServer, _ := modelTurnServer(t)
	invalidServer.WithEdgeStore(continuationStore(testWorkspaceID, "sandbox", "htb-linux"))
	if got := callToolErrorText(t, invalidServer, "workspace_runtime_continue", `{"workspace_id":"`+testWorkspaceID+`","timeout_seconds":300}`); got != modelturn.ErrInvalidRequest.Error() {
		t.Fatalf("invalid mode error=%q", got)
	}
	for _, timeout := range []int{0, 3601} {
		response := call(t, server, continueCall("timeout", testWorkspaceID, timeout, ""))
		var result toolResult
		encoded, _ := json.Marshal(response.Result)
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			t.Fatalf("timeout=%d result=%s", timeout, encoded)
		}
	}
}

func TestWorkspaceRuntimeContinueRejectsInactiveEdgeBinding(t *testing.T) {
	server, _ := modelTurnServer(t)
	store := continuationStore(testWorkspaceID, "linux-workcell", "dev")
	store.devices[testEdgeDeviceID] = false
	server.WithEdgeStore(store)
	got := callToolErrorText(t, server, "workspace_runtime_continue", `{"workspace_id":"`+testWorkspaceID+`","timeout_seconds":300}`)
	if !strings.Contains(got, "active workspace not found") {
		t.Fatalf("inactive edge error=%q", got)
	}
}
