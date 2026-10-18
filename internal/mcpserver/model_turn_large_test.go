package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelTurnNextDoesNotStageLargeCanonicalRequest(t *testing.T) {
	server, store := modelTurnServer(t)
	start := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"model_runtime_start","arguments":{}}}`))
	var runtime modelturn.Runtime
	if err := json.Unmarshal([]byte(start), &runtime); err != nil {
		t.Fatal(err)
	}
	marker := "model-request-marker-"
	payload := json.RawMessage(`{"messages":[{"role":"user","content":"` + marker + strings.Repeat("x", largeResultThresholdBytes+4096) + `"}]}`)
	if _, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{RuntimeID: runtime.RuntimeID, Sequence: 1, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	next := toolText(t, call(t, server, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"model_turn_next","arguments":{"runtime_id":"`+runtime.RuntimeID+`"}}}`))
	if !strings.Contains(next, marker) || strings.Contains(next, `"result_ref"`) || len(next) <= largeResultThresholdBytes {
		t.Fatalf("large model turn was compacted: bytes=%d body=%s", len(next), next[:min(len(next), 512)])
	}
}
