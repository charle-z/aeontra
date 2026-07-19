package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
)

func modelTurnServer(t *testing.T) (*Server, *modelturn.Store) {
	t.Helper()
	store, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "model-turns")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	results, err := resultstore.Open(resultstore.Config{Root: filepath.Join(t.TempDir(), "results")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = results.Close() })
	server := stampServer(t)
	server.svc.WithResultStore(results)
	return server.WithModelTurnStore(store), store
}

func TestModelTurnToolsFailClosedWithoutStore(t *testing.T) {
	server := stampServer(t)
	for name, arguments := range map[string]string{
		"model_runtime_start":        `{}`,
		"workspace_runtime_continue": `{"workspace_id":"ws_22222222222222222222222222222222","timeout_seconds":60}`,
		"opencode_runtime_start":     `{"device_id":"ed_11111111111111111111111111111111","workspace_id":"ws_22222222222222222222222222222222","goal":"bounded","timeout_seconds":60,"idempotency_key":"key-1"}`,
		"model_runtime_status":       `{"runtime_id":"mr_00000000000000000000000000000000"}`,
		"model_turn_next":            `{"runtime_id":"mr_00000000000000000000000000000000"}`,
		"model_turn_respond":         `{"runtime_id":"mr_00000000000000000000000000000000","turn_id":"mt_00000000000000000000000000000000","expected_sequence":1,"request_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","response":{"finish_reason":"stop"}}`,
		"model_runtime_cancel":       `{"runtime_id":"mr_00000000000000000000000000000000"}`,
	} {
		response := call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+arguments+`}}`)
		var result toolResult
		encoded, _ := json.Marshal(response.Result)
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if !result.IsError || len(result.Content) != 1 || result.Content[0].Text != errModelTurnStoreUnavailable.Error() {
			t.Fatalf("%s result=%s", name, encoded)
		}
	}
}

func TestModelTurnToolsRunBoundedPullWorkflow(t *testing.T) {
	server, store := modelTurnServer(t)
	start := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"model_runtime_start","arguments":{}}}`))
	var runtime modelturn.Runtime
	if err := json.Unmarshal([]byte(start), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.RuntimeID == "" || runtime.Status != modelturn.RuntimeReady {
		t.Fatalf("runtime=%+v", runtime)
	}

	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtime.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"messages":[{"role":"user","content":"inspect files"}]}`),
		OfferedTools: []modelturn.ToolDefinition{
			{ID: "tool-read", Name: "read_file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	next := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"model_turn_next","arguments":{"runtime_id":"`+runtime.RuntimeID+`"}}}`))
	if !strings.Contains(next, `"pending":true`) || !strings.Contains(next, `"turn_id":"`+string(turn.ID)+`"`) || !strings.Contains(next, `"content":"inspect files"`) || !strings.Contains(next, `"offered_tool_ids":["tool-read"]`) {
		t.Fatalf("next=%s", next)
	}

	invented := call(t, server, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"model_turn_respond","arguments":{"runtime_id":"`+runtime.RuntimeID+`","turn_id":"`+string(turn.ID)+`","expected_sequence":1,"request_digest":"`+turn.RequestDigest+`","response":{"finish_reason":"tool_calls","tool_calls":[{"call_id":"call-1","tool_id":"invented-tool","arguments":{}}]}}}}`)
	var inventedResult toolResult
	encoded, _ := json.Marshal(invented.Result)
	if err := json.Unmarshal(encoded, &inventedResult); err != nil {
		t.Fatal(err)
	}
	if !inventedResult.IsError || !strings.Contains(inventedResult.Content[0].Text, modelturn.ErrToolNotOffered.Error()) {
		t.Fatalf("invented result=%s", encoded)
	}

	responded := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"model_turn_respond","arguments":{"runtime_id":"`+runtime.RuntimeID+`","turn_id":"`+string(turn.ID)+`","expected_sequence":1,"request_digest":"`+turn.RequestDigest+`","response":{"finish_reason":"tool_calls","tool_calls":[{"call_id":"call-1","tool_id":"tool-read","arguments":{"path":"README.md"}}]}}}}`))
	if !strings.Contains(responded, `"status":"responded"`) {
		t.Fatalf("responded=%s", responded)
	}
	response, err := store.WaitResponse(context.Background(), turn.ID)
	if err != nil || !strings.Contains(string(response.Payload), `"tool_id":"tool-read"`) {
		t.Fatalf("response=%s err=%v", response.Payload, err)
	}

	status := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"model_runtime_status","arguments":{"runtime_id":"`+runtime.RuntimeID+`"}}}`))
	if !strings.Contains(status, `"last_sequence":1`) || strings.Contains(status, `"active_turn_status"`) || strings.Contains(status, `"goal_summary"`) {
		t.Fatalf("status=%s", status)
	}
}

func TestModelTurnRespondRejectsUnknownFieldsAndInventedSchemas(t *testing.T) {
	server, _ := modelTurnServer(t)
	start := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"model_runtime_start","arguments":{}}}`))
	var runtime modelturn.Runtime
	if err := json.Unmarshal([]byte(start), &runtime); err != nil {
		t.Fatal(err)
	}
	response := call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"model_turn_respond","arguments":{"runtime_id":"`+runtime.RuntimeID+`","turn_id":"mt_00000000000000000000000000000000","expected_sequence":1,"request_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","response":{"finish_reason":"stop","schema":{"type":"object"}}}}}`)
	var result toolResult
	encoded, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "unknown field") {
		t.Fatalf("schema injection result=%s", encoded)
	}
}

func TestModelRuntimeCancelCancelsPendingTurn(t *testing.T) {
	server, store := modelTurnServer(t)
	start := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"model_runtime_start","arguments":{}}}`))
	var runtime modelturn.Runtime
	if err := json.Unmarshal([]byte(start), &runtime); err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{RuntimeID: runtime.RuntimeID, Sequence: 1, Payload: json.RawMessage(`{"messages":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	cancelled := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"model_runtime_cancel","arguments":{"runtime_id":"`+runtime.RuntimeID+`"}}}`))
	if !strings.Contains(cancelled, `"state":"cancelled"`) || strings.Contains(cancelled, `"status"`) {
		t.Fatalf("cancelled=%s", cancelled)
	}
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil || record.Status != modelturn.StatusCancelled {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if _, err := store.WaitResponse(context.Background(), turn.ID); !errors.Is(err, modelturn.ErrTurnConflict) {
		t.Fatalf("wait error=%v", err)
	}
}
