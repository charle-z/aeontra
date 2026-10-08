package mcpserver

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestCodexRuntimeStartContractIsClosedAndIdempotent(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	entry, ok := server.table["codex_runtime_start"]
	if !ok {
		t.Fatal("codex_runtime_start is not registered")
	}
	if entry.def.Version != "1" || entry.def.InputSchema["additionalProperties"] != false {
		t.Fatalf("unexpected Codex runtime contract: %#v", entry.def)
	}
	properties := entry.def.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"device_id", "goal", "idempotency_key", "timeout_seconds", "workspace_id"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("properties=%v", keys)
	}
	arguments := openCodeStartArguments("Inspect the project and run its tests.", "codex-goal-1")
	first := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"codex_runtime_start","arguments":`+arguments+`}}`))
	second := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"codex_runtime_start","arguments":`+arguments+`}}`))
	if first != second {
		t.Fatalf("idempotent Codex runtime changed\nfirst=%s\nsecond=%s", first, second)
	}
	var view runtimePublicView
	if err := json.Unmarshal([]byte(first), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != modelturn.RuntimeStateAwaitingEdge || view.DeviceID != testEdgeDeviceID || view.WorkspaceID != testWorkspaceID {
		t.Fatalf("view=%+v", view)
	}
}
