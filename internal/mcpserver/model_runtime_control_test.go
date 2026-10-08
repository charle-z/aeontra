package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRuntimeControlIsOptInAndFencesResponses(t *testing.T) {
	server, store := modelTurnServer(t)
	if _, exists := server.table["model_runtime_control"]; !exists {
		t.Fatal("no durable controller handoff tool is offered")
	}
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{RuntimeID: runtime.RuntimeID, Sequence: 1, Payload: json.RawMessage(`{"prompt":"goal"}`)})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"runtime_id": runtime.RuntimeID, "action": "claim", "controller_id": "mc_11111111111111111111111111111111", "generation": 0, "turn_id": turn.ID, "expected_sequence": turn.Sequence, "request_digest": turn.RequestDigest})
	result, err := server.table["model_runtime_control"].handler(args)
	if err != nil {
		t.Fatal(err)
	}
	var control struct {
		Generation uint64 `json:"generation"`
	}
	if err := json.Unmarshal([]byte(result), &control); err != nil || control.Generation != 1 {
		t.Fatalf("control=%s err=%v", result, err)
	}
	response, _ := json.Marshal(map[string]any{"runtime_id": runtime.RuntimeID, "turn_id": turn.ID, "expected_sequence": turn.Sequence, "request_digest": turn.RequestDigest, "response": map[string]any{"finish_reason": "stop"}})
	if _, err := server.handleModelTurnRespond(response); err == nil {
		t.Fatal("legacy response bypassed opt-in controller")
	}
	var owned map[string]any
	_ = json.Unmarshal(response, &owned)
	owned["controller_id"], owned["control_generation"] = "mc_11111111111111111111111111111111", 1
	response, _ = json.Marshal(owned)
	if _, err := server.handleModelTurnRespond(response); err != nil {
		t.Fatal(err)
	}
}
