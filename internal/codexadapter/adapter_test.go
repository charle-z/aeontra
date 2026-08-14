package codexadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

type adapterTransport struct {
	mu        sync.Mutex
	runtime   modelturn.Runtime
	request   modelturn.ModelRequest
	response  modelturn.ModelResponse
	created   int
	cancelled int
	wait      func(context.Context, modelturn.TurnID) (modelturn.ModelResponse, error)
}

func (t *adapterTransport) Runtime(context.Context, string) (modelturn.Runtime, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runtime, nil
}

func (t *adapterTransport) CreateTurn(_ context.Context, request modelturn.ModelRequest) (modelturn.Turn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.created++
	t.request = request
	return modelturn.Turn{
		RuntimeID: request.RuntimeID, ID: modelturn.TurnID("mt_11111111111111111111111111111111"),
		Sequence: request.Sequence, RequestDigest: request.RequestDigest,
	}, nil
}

func (t *adapterTransport) WaitResponse(ctx context.Context, turnID modelturn.TurnID) (modelturn.ModelResponse, error) {
	if t.wait != nil {
		return t.wait(ctx, turnID)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	response := t.response
	response.RuntimeID = t.request.RuntimeID
	response.TurnID = turnID
	response.Sequence = t.request.Sequence
	response.RequestDigest = t.request.RequestDigest
	return response, nil
}

func (t *adapterTransport) Cancel(context.Context, modelturn.TurnID) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelled++
	return nil
}

func TestAdapterTranslatesStockResponsesRequestAndStreamsToolCall(t *testing.T) {
	transport := validAdapterTransport()
	schema := json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"],"additionalProperties":false}`)
	execToolID, err := toolID("exec_command", schema)
	if err != nil {
		t.Fatal(err)
	}
	transport.response.Payload = json.RawMessage(`{"text":"checking","tool_calls":[{"call_id":"call_1","tool_id":"` + execToolID + `","arguments":{"cmd":"go test ./..."}}],"finish_reason":"tool_calls","usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19}}`)
	adapter, err := New(Options{
		RuntimeID: "mr_00000000000000000000000000000000",
		ModelID:   "mcp-devbox-codex",
		Transport: transport,
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{
		"model":"mcp-devbox-codex",
		"instructions":"work inside the repository",
		"input":[
			{"type":"message","id":"msg_dev","role":"developer","content":[{"type":"input_text","text":"developer contract"}]},
			{"type":"message","id":"msg_user","role":"user","content":[{"type":"input_text","text":"run tests"}]}
		],
		"tools":[
			{"type":"function","name":"exec_command","description":"run argv","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"],"additionalProperties":false},"strict":false},
			{"type":"namespace","name":"multi_agent_v1","description":"deferred","tools":[{"type":"function","name":"spawn_agent","description":"deferred","parameters":{"type":"object"},"strict":false}]},
			{"type":"web_search","external_web_access":false}
		],
		"tool_choice":"auto",
		"parallel_tool_calls":false,
		"reasoning":{"summary":"auto"},
		"store":false,
		"stream":true,
		"include":["reasoning.encrypted_content"],
		"prompt_cache_key":"opaque-cache-key",
		"client_metadata":{"session_id":"must-not-cross","turn_id":"must-not-cross"}
	}`
	var parsed responsesRequest
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeResponsesRequest(parsed); err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	request := newResponsesRequest(t, body)
	recorder := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type=%q", got)
	}
	for _, marker := range []string{"response.created", "response.output_item.done", `"type":"function_call"`, `"name":"exec_command"`, `"call_id":"call_1"`, "response.completed"} {
		if !strings.Contains(recorder.Body.String(), marker) {
			t.Errorf("SSE response missing %q: %s", marker, recorder.Body.String())
		}
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.created != 1 || transport.request.Sequence != 1 || !transport.request.CanonicalPayload {
		t.Fatalf("created=%d request=%+v", transport.created, transport.request)
	}
	if transport.request.RequestDigest == "" || len(transport.request.OfferedTools) != 1 {
		t.Fatalf("request identity/tools missing: %+v", transport.request)
	}
	var payload map[string]any
	if err := json.Unmarshal(transport.request.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	encoded := string(transport.request.Payload)
	if strings.Contains(encoded, "must-not-cross") || strings.Contains(encoded, "opaque-cache-key") || strings.Contains(encoded, "reasoning.encrypted_content") || strings.Contains(encoded, "multi_agent_v1") || strings.Contains(encoded, "web_search") {
		t.Fatalf("private Codex metadata crossed model boundary: %s", encoded)
	}
	if payload["protocol_version"] != ProtocolVersion || payload["model_id"] != "mcp-devbox-codex" {
		t.Fatalf("payload identity=%v", payload)
	}
}

func TestAdapterRejectsUnknownFieldsAndNonLoopbackRequests(t *testing.T) {
	transport := validAdapterTransport()
	adapter, err := New(Options{RuntimeID: transport.runtime.RuntimeID, ModelID: "mcp-devbox-codex", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		body       string
		remoteAddr string
		host       string
		auth       string
		want       int
	}{
		{name: "unknown", body: `{"model":"mcp-devbox-codex","input":[],"stream":true,"unknown":true}`, remoteAddr: "127.0.0.1:1234", host: "127.0.0.1:4321", want: http.StatusBadRequest},
		{name: "remote", body: `{"model":"mcp-devbox-codex","input":[],"stream":true}`, remoteAddr: "192.0.2.10:1234", host: "127.0.0.1:4321", want: http.StatusForbidden},
		{name: "host", body: `{"model":"mcp-devbox-codex","input":[],"stream":true}`, remoteAddr: "127.0.0.1:1234", host: "example.com", want: http.StatusForbidden},
		{name: "authorization", body: `{"model":"mcp-devbox-codex","input":[],"stream":true}`, remoteAddr: "127.0.0.1:1234", host: "127.0.0.1:4321", auth: "Bearer forbidden", want: http.StatusBadRequest},
		{name: "nonstream", body: `{"model":"mcp-devbox-codex","input":[],"stream":false}`, remoteAddr: "127.0.0.1:1234", host: "127.0.0.1:4321", want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newResponsesRequest(t, test.body)
			request.RemoteAddr = test.remoteAddr
			request.Host = test.host
			if test.auth != "" {
				request.Header.Set("Authorization", test.auth)
			}
			recorder := httptest.NewRecorder()
			adapter.Handler().ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.created != 0 {
		t.Fatalf("rejected requests created %d turns", transport.created)
	}
}

func TestAdapterCancelsTurnWhenClientDisconnects(t *testing.T) {
	transport := validAdapterTransport()
	waiting := make(chan struct{})
	transport.wait = func(ctx context.Context, _ modelturn.TurnID) (modelturn.ModelResponse, error) {
		close(waiting)
		<-ctx.Done()
		return modelturn.ModelResponse{}, ctx.Err()
	}
	adapter, err := New(Options{RuntimeID: transport.runtime.RuntimeID, ModelID: "mcp-devbox-codex", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := newResponsesRequest(t, `{"model":"mcp-devbox-codex","input":[],"stream":true}`).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		adapter.Handler().ServeHTTP(httptest.NewRecorder(), request)
		close(done)
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("adapter did not start waiting for the model response")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("adapter did not stop after client cancellation")
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.created != 1 || transport.cancelled != 1 {
		t.Fatalf("created=%d cancelled=%d", transport.created, transport.cancelled)
	}
}

func TestAdapterRejectsUnofferedModelToolAndCancelsTurn(t *testing.T) {
	transport := validAdapterTransport()
	transport.response.Payload = json.RawMessage(`{"tool_calls":[{"call_id":"call_1","tool_id":"tool_aaaaaaaaaaaaaaaaaaaaaaaa","arguments":{}}],"finish_reason":"tool_calls"}`)
	adapter, err := New(Options{RuntimeID: transport.runtime.RuntimeID, ModelID: "mcp-devbox-codex", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	adapter.Handler().ServeHTTP(recorder, newResponsesRequest(t, `{"model":"mcp-devbox-codex","input":[],"stream":true}`))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.created != 1 || transport.cancelled != 1 {
		t.Fatalf("created=%d cancelled=%d", transport.created, transport.cancelled)
	}
}

func validAdapterTransport() *adapterTransport {
	const runtimeID = "mr_00000000000000000000000000000000"
	return &adapterTransport{
		runtime: modelturn.Runtime{RuntimeID: runtimeID, Status: modelturn.RuntimeRunning},
		response: modelturn.ModelResponse{
			RuntimeID: runtimeID, TurnID: modelturn.TurnID("mt_11111111111111111111111111111111"), Sequence: 1,
			Payload: json.RawMessage(`{"text":"done","finish_reason":"stop"}`),
		},
	}
}

func newResponsesRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4321/v1/responses", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "127.0.0.1:1234"
	return request
}
