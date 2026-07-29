package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

const (
	testEdgeDeviceID = "ed_11111111111111111111111111111111"
	testWorkspaceID  = "ws_22222222222222222222222222222222"
)

type fixedEdgeDevices map[string]bool

func (devices fixedEdgeDevices) DeviceActive(deviceID string) bool {
	return devices[deviceID]
}

func openCodeStartArguments(goal, key string) string {
	payload, _ := json.Marshal(map[string]any{
		"device_id": testEdgeDeviceID, "workspace_id": testWorkspaceID,
		"goal": goal, "timeout_seconds": 300, "idempotency_key": key,
	})
	return string(payload)
}

func callToolErrorText(t *testing.T, server *Server, name, arguments string) string {
	t.Helper()
	response := call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+arguments+`}}`)
	var result toolResult
	encoded, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("expected tool error for %s: %s", name, encoded)
	}
	return result.Content[0].Text
}

func TestOpenCodeRuntimeStartContractIsClosedAndBounded(t *testing.T) {
	server := stampServer(t)
	entry, ok := server.table["opencode_runtime_start"]
	if !ok {
		t.Fatal("opencode_runtime_start is not registered")
	}
	if entry.def.Version != "1" {
		t.Fatalf("version=%q", entry.def.Version)
	}
	wantHints := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	if !reflect.DeepEqual(entry.def.Annotations, wantHints) {
		t.Fatalf("annotations=%#v", entry.def.Annotations)
	}
	if entry.def.InputSchema["additionalProperties"] != false {
		t.Fatalf("schema is not closed: %#v", entry.def.InputSchema)
	}
	properties := entry.def.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	wantKeys := []string{"device_id", "goal", "idempotency_key", "timeout_seconds", "workspace_id"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("properties=%v", keys)
	}
	for _, forbidden := range []string{"path", "shell", "argv", "env", "provider", "api_key", "model", "mount", "uid", "vpn", "sudo", "network_policy", "provider_url"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("forbidden property %q", forbidden)
		}
	}
}

func TestOpenCodeRuntimeStartRequiresConfiguredActiveDevice(t *testing.T) {
	server, _ := modelTurnServer(t)
	arguments := openCodeStartArguments("Inspect the fixture and run its tests.", "goal-1")
	if got := callToolErrorText(t, server, "opencode_runtime_start", arguments); got != errEdgeStoreUnavailable.Error() {
		t.Fatalf("missing edge store error=%q", got)
	}
	server.WithEdgeStore(fixedEdgeDevices{})
	if got := callToolErrorText(t, server, "opencode_runtime_start", arguments); got != "active edge device not found" {
		t.Fatalf("inactive device error=%q", got)
	}
}

func TestOpenCodeRuntimeStartIsIdempotentAndPubliclyRedacted(t *testing.T) {
	server, store := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	goal := "Read the bounded fixture, edit it, and run go test ./...."
	arguments := openCodeStartArguments(goal, "goal-2")
	firstText := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"opencode_runtime_start","arguments":`+arguments+`}}`))
	secondText := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"opencode_runtime_start","arguments":`+arguments+`}}`))
	if firstText != secondText {
		t.Fatalf("idempotent response changed\nfirst=%s\nsecond=%s", firstText, secondText)
	}
	var public map[string]json.RawMessage
	if err := json.Unmarshal([]byte(firstText), &public); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(public))
	for key := range public {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	wantKeys := []string{"controller", "device_id", "last_sequence", "phases", "runtime_id", "state", "updated_at", "workspace_id"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("public keys=%v body=%s", keys, firstText)
	}
	for _, forbidden := range []string{"goal", "goal_summary", "status", "created_at", "expires_at", "last_heartbeat", "active_turn_id", "active_turn_status", "path", "command", "prompt", "result"} {
		if strings.Contains(firstText, forbidden) {
			t.Fatalf("public response contains %q: %s", forbidden, firstText)
		}
	}
	var view runtimePublicView
	if err := json.Unmarshal([]byte(firstText), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != modelturn.RuntimeStateAwaitingEdge || view.Controller != modelturn.ControllerRemoteEdge || view.DeviceID != testEdgeDeviceID || view.WorkspaceID != testWorkspaceID {
		t.Fatalf("view=%+v", view)
	}
	if len(view.Phases) != 1 || view.Phases[0].Phase != modelturn.RuntimePhaseCreated || view.Phases[0].DurationMS != 0 || view.Phases[0].SinceCreatedMS != 0 {
		t.Fatalf("phases=%+v", view.Phases)
	}
	runtime, err := store.Runtime(context.Background(), view.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	storedGoal, digest, err := store.RuntimeGoal(context.Background(), view.RuntimeID, testEdgeDeviceID)
	if err != nil || string(storedGoal) != goal || digest == "" || runtime.GoalSummary != modelturn.GoalSummary([]byte(goal)) {
		t.Fatalf("runtime=%+v goal=%q digest=%q err=%v", runtime, storedGoal, digest, err)
	}
}

func TestOpenCodeRuntimeStartRejectsConflictsAndForbiddenFields(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	first := openCodeStartArguments("First bounded goal.", "same-key")
	_ = toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"opencode_runtime_start","arguments":`+first+`}}`))
	changed := openCodeStartArguments("Different bounded goal.", "same-key")
	if got := callToolErrorText(t, server, "opencode_runtime_start", changed); !strings.Contains(got, modelturn.ErrTurnConflict.Error()) {
		t.Fatalf("changed idempotent request error=%q", got)
	}
	for _, forbidden := range []string{"path", "shell", "argv", "env", "provider", "api_key", "model", "mount", "uid", "vpn", "sudo", "network_policy"} {
		var object map[string]any
		if err := json.Unmarshal([]byte(openCodeStartArguments("Bounded goal.", "key-"+forbidden)), &object); err != nil {
			t.Fatal(err)
		}
		object[forbidden] = "forbidden-secret-value"
		encoded, _ := json.Marshal(object)
		got := callToolErrorText(t, server, "opencode_runtime_start", string(encoded))
		if !strings.Contains(got, "unknown field") || strings.Contains(got, "forbidden-secret-value") {
			t.Fatalf("field=%s error=%q", forbidden, got)
		}
	}
}

func TestRemoteRuntimeStatusAndCancelExposeOnlyPublicView(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	start := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"opencode_runtime_start","arguments":`+openCodeStartArguments("Bounded goal.", "goal-3")+`}}`))
	var view runtimePublicView
	if err := json.Unmarshal([]byte(start), &view); err != nil {
		t.Fatal(err)
	}
	status := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"model_runtime_status","arguments":{"runtime_id":"`+view.RuntimeID+`"}}}`))
	if status != start {
		t.Fatalf("status changed public view\nstart=%s\nstatus=%s", start, status)
	}
	cancelled := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"model_runtime_cancel","arguments":{"runtime_id":"`+view.RuntimeID+`"}}}`))
	repeated := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"model_runtime_cancel","arguments":{"runtime_id":"`+view.RuntimeID+`"}}}`))
	if cancelled != repeated || !strings.Contains(cancelled, `"state":"cancelled"`) || strings.Contains(cancelled, `"status"`) || strings.Contains(cancelled, "goal") {
		t.Fatalf("cancelled=%s repeated=%s", cancelled, repeated)
	}
}

func TestRemoteOpenCodeRuntimeCannotTargetAuthorizedHTBWorkspace(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "htb-linux"))
	got := callToolErrorText(t, server, "opencode_runtime_start", openCodeStartArguments("Caller supplied operational goal.", "htb-forbidden"))
	if got != "registered development workspace not found" {
		t.Fatalf("HTB runtime error=%q", got)
	}
}
