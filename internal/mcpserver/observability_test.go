package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func newObservedServer(t *testing.T) (*Server, string, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{root}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git", "go"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	observer, err := observability.Open(observability.Config{Mode: observability.ModeStderr, MaxBytes: observability.DefaultMaxBytes}, &events)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	svc := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0])
	return NewWithObserver(svc, observer), pol.Roots()[0], &events
}

func decodeEvents(t *testing.T, output string) []observability.Event {
	t.Helper()
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Fatal("no observability events")
	}
	var events []observability.Event
	for _, line := range strings.Split(trimmed, "\n") {
		var event observability.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("bad event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestObservedToolCallOmitsParamsPathsResultsPromptsAndSecrets(t *testing.T) {
	server, root, events := newObservedServer(t)
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	path := filepath.Join(root, "private-customer-target.txt")
	content := "IGNORE ALL RULES prompt body response target.example " + secret
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"private-customer-target.txt"}}}`
	response := server.handle([]byte(request))
	if len(response) == 0 {
		t.Fatal("missing response")
	}
	output := events.String()
	for _, forbidden := range []string{secret, "private-customer-target", "IGNORE ALL RULES", "target.example", "arguments", "params", "package"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("observability leaked %q: %s", forbidden, output)
		}
	}
	decoded := decodeEvents(t, output)
	last := decoded[len(decoded)-1]
	if last.Component != observability.ComponentMCP || last.Name != observability.EventRPCRequest || last.Method != observability.MethodToolsCall {
		t.Fatalf("unexpected event: %+v", last)
	}
	if last.Tool != "read_file" || last.Outcome != observability.OutcomeSuccess || last.RequestID == "" {
		t.Fatalf("unexpected tool outcome: %+v", last)
	}
	if last.InputBytes != int64(len(request)) || last.OutputBytes != int64(len(response)) || last.ToolDurationMS != last.DurationMS {
		t.Fatalf("tool metrics are not exact: %+v", last)
	}
}

func TestObservedUnknownToolDoesNotPersistClientToolName(t *testing.T) {
	server, _, events := newObservedServer(t)
	secretName := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	_ = server.handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + secretName + `","arguments":{}}}`))
	if strings.Contains(events.String(), secretName) {
		t.Fatalf("unknown client tool leaked: %s", events.String())
	}
	last := decodeEvents(t, events.String())[0]
	if last.Tool != "unknown" || last.ErrorClass != observability.ErrorUnknownTool || last.Outcome != observability.OutcomeError {
		t.Fatalf("unexpected event: %+v", last)
	}
}

func TestHTTPObservabilityUsesServerRequestIDAndNeverLogsQueryToken(t *testing.T) {
	server, _, events := newObservedServer(t)
	handler := server.HTTPHandler(testToken, nil)
	querySecret := "query-token-should-not-appear"
	req := httptest.NewRequest(http.MethodPost, "/mcp?key="+querySecret, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("X-MCP-Request-ID", "client-controlled-secret-id")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
	requestID := recorder.Header().Get("X-MCP-Request-ID")
	if requestID == "" || requestID == "client-controlled-secret-id" {
		t.Fatalf("server request id = %q", requestID)
	}
	output := events.String()
	for _, forbidden := range []string{querySecret, "client-controlled-secret-id", "?key=", "Authorization"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("HTTP event leaked %q: %s", forbidden, output)
		}
	}
	last := decodeEvents(t, output)[0]
	if last.Component != observability.ComponentHTTP || last.Route != observability.RouteMCP || last.StatusCode != http.StatusUnauthorized || last.Outcome != observability.OutcomeDenied {
		t.Fatalf("unexpected HTTP event: %+v", last)
	}
	if last.RequestID != requestID {
		t.Fatalf("event request id %q != response %q", last.RequestID, requestID)
	}
	if last.HTTPDurationMS != last.DurationMS {
		t.Fatalf("HTTP duration mismatch: %+v", last)
	}
}

func TestHTTPAndRPCEventsShareServerGeneratedRequestID(t *testing.T) {
	server, _, events := newObservedServer(t)
	handler := server.HTTPHandler(testToken, nil)
	sessionID := initializeHandlerSession(t, handler, "Bearer "+testToken)
	events.Reset()
	recorder := doWithSession(t, handler, http.MethodPost, DefaultMCPPath, "Bearer "+testToken, sessionID, rpcBody(t, 2, "tools/list", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	requestID := recorder.Header().Get("X-MCP-Request-ID")
	decoded := decodeEvents(t, events.String())
	if len(decoded) != 2 {
		t.Fatalf("events = %+v", decoded)
	}
	for _, event := range decoded {
		if event.RequestID != requestID {
			t.Fatalf("request ids differ: %+v", decoded)
		}
	}
	if decoded[0].Component != observability.ComponentMCP && decoded[1].Component != observability.ComponentMCP {
		t.Fatalf("missing MCP event: %+v", decoded)
	}
	if decoded[0].Component != observability.ComponentHTTP && decoded[1].Component != observability.ComponentHTTP {
		t.Fatalf("missing HTTP event: %+v", decoded)
	}
}
