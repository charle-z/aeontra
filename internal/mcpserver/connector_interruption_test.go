package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestConnectorSessionInterruptionPreservesDurableModelTurn(t *testing.T) {
	server, store := modelTurnServer(t)
	handler := server.HTTPHandler(testToken, nil)
	firstSession := initializeHandlerSession(t, handler, "Bearer "+testToken)

	start := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, firstSession,
		rpcBody(t, 2, "tools/call", map[string]any{"name": "model_runtime_start", "arguments": map[string]any{}}))
	if start.Code != http.StatusOK {
		t.Fatalf("model_runtime_start status=%d body=%s", start.Code, start.Body.String())
	}
	var runtime modelturn.Runtime
	if err := json.Unmarshal([]byte(httpToolText(t, start.Body.Bytes())), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.RuntimeID == "" {
		t.Fatal("model_runtime_start returned no runtime id")
	}

	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtime.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"messages":[{"role":"user","content":"continue after reconnect"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted := doWithSession(t, handler, http.MethodDelete, DefaultMCPPath, "Bearer "+testToken, firstSession, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete first session status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	stale := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, firstSession,
		rpcBody(t, 3, "tools/list", nil))
	if stale.Code != http.StatusNotFound {
		t.Fatalf("revoked session status=%d body=%s", stale.Code, stale.Body.String())
	}

	secondSession := initializeHandlerSession(t, handler, "Bearer "+testToken)
	if secondSession == firstSession {
		t.Fatal("connector reinitialization reused the revoked session")
	}
	next := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, secondSession,
		rpcBody(t, 4, "tools/call", map[string]any{
			"name": "model_turn_next", "arguments": map[string]any{"runtime_id": runtime.RuntimeID},
		}))
	if next.Code != http.StatusOK {
		t.Fatalf("model_turn_next status=%d body=%s", next.Code, next.Body.String())
	}
	text := httpToolText(t, next.Body.Bytes())
	for _, expected := range []string{
		`"pending":true`,
		`"turn_id":"` + string(turn.ID) + `"`,
		`"request_digest":"` + turn.RequestDigest + `"`,
		`"sequence":1`,
		`"content":"continue after reconnect"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("resumed turn missing %s: %s", expected, text)
		}
	}
}

func httpToolText(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Result toolResult `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Result.IsError || len(envelope.Result.Content) != 1 || envelope.Result.Content[0].Type != "text" {
		t.Fatalf("invalid tool result: %s", body)
	}
	return envelope.Result.Content[0].Text
}
