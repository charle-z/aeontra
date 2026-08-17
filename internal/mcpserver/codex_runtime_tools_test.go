package mcpserver

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestCodexRuntimeStartContractIsClosedAndIdempotent(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	entry, ok := server.table["codex_runtime_start"]
	if !ok {
		t.Fatal("codex_runtime_start is not registered")
	}
	if entry.def.Version != "2" || entry.def.InputSchema["additionalProperties"] != false {
		t.Fatalf("unexpected Codex runtime contract: %#v", entry.def)
	}
	properties := entry.def.InputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"alias", "device_id", "goal", "idempotency_key", "target", "timeout_seconds", "workspace_id"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("properties=%v", keys)
	}
	required := append([]string(nil), entry.def.InputSchema["required"].([]string)...)
	sort.Strings(required)
	wantRequired := []string{"goal", "idempotency_key", "timeout_seconds"}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("required=%v", required)
	}
	oneOf, ok := entry.def.InputSchema["oneOf"].([]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("oneOf=%#v", entry.def.InputSchema["oneOf"])
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

func TestCodexRuntimeStartResolvesRegisteredProjectAlias(t *testing.T) {
	server, _ := modelTurnServer(t)
	const (
		alias  = "codex-36010"
		target = "parrot-trusted-linux"
	)
	store := continuationStore(testWorkspaceID, "linux-workcell", "dev")
	store.aliases = map[string]edge.Device{
		target: {ID: testEdgeDeviceID, Name: target},
	}
	store.projects = map[string]edge.WorkspaceBinding{
		testEdgeDeviceID + "|" + alias + "|" + target: {
			WorkspaceID: testWorkspaceID,
			DeviceID:    testEdgeDeviceID,
			Profile:     "linux-workcell",
			Mode:        "dev",
		},
	}
	server.WithEdgeStore(store)
	encoded, err := json.Marshal(openCodeRuntimeStartParams{
		Alias:          alias,
		Target:         target,
		Goal:           "Recover the registered project workspace.",
		TimeoutSeconds: 300,
		IdempotencyKey: "codex-project-alias-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := server.handleOpenCodeRuntimeStart(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var view runtimePublicView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatal(err)
	}
	if view.State != modelturn.RuntimeStateAwaitingEdge || view.DeviceID != testEdgeDeviceID || view.WorkspaceID != testWorkspaceID {
		t.Fatalf("view=%+v", view)
	}
}

func TestCodexRuntimeStartRejectsMixedOrIncompleteWorkspaceIdentity(t *testing.T) {
	server, _ := modelTurnServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	tests := []openCodeRuntimeStartParams{
		{DeviceID: testEdgeDeviceID, WorkspaceID: testWorkspaceID, Alias: "codex", Target: "parrot-trusted-linux"},
		{DeviceID: testEdgeDeviceID},
		{WorkspaceID: testWorkspaceID},
		{Alias: "codex"},
		{Target: "parrot-trusted-linux"},
		{},
	}
	for index, params := range tests {
		params.Goal = "Invalid identity combination."
		params.TimeoutSeconds = 300
		params.IdempotencyKey = "codex-invalid-identity"
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.handleOpenCodeRuntimeStart(encoded); !errors.Is(err, modelturn.ErrInvalidRequest) {
			t.Fatalf("case %d err=%v", index, err)
		}
	}
}
