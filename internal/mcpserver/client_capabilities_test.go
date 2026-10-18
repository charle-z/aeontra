package mcpserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

var capabilityKeys = []string{
	"client_name",
	"client_version",
	"protocol_version",
	"sampling_supported",
	"roots_supported",
	"elicitation_supported",
	"observed_at",
}

func TestParseClientCapabilitiesSamplingPresent(t *testing.T) {
	observed := time.Date(2026, 7, 15, 8, 0, 0, 123, time.UTC)
	got := parseClientCapabilities(json.RawMessage(`{
		"protocolVersion":"2025-06-18",
		"clientInfo":{"name":"chat-client","version":"1.2.3"},
		"capabilities":{"sampling":{},"roots":{"listChanged":true},"elicitation":{}}
	}`), observed)
	want := ClientCapabilities{
		ClientName:           "chat-client",
		ClientVersion:        "1.2.3",
		ProtocolVersion:      "2025-06-18",
		SamplingSupported:    true,
		RootsSupported:       true,
		ElicitationSupported: true,
		ObservedAt:           observed.Format(time.RFC3339Nano),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities=%+v want=%+v", got, want)
	}
}

func TestParseClientCapabilitiesSamplingAbsentAndUnknownIgnored(t *testing.T) {
	got := parseClientCapabilities(json.RawMessage(`{
		"protocolVersion":"2025-06-18",
		"clientInfo":{"name":"client","version":"2"},
		"capabilities":{"futureCapability":{"enabled":true},"sampling":null,"roots":"invalid"}
	}`), time.Unix(1, 0))
	if got.SamplingSupported || got.RootsSupported || got.ElicitationSupported {
		t.Fatalf("unsupported capability inferred: %+v", got)
	}
}

func TestParseClientCapabilitiesMissingClientInfoAndMalformedPayload(t *testing.T) {
	observed := time.Unix(2, 0).UTC()
	withoutInfo := parseClientCapabilities(json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`), observed)
	if withoutInfo.ClientName != "" || withoutInfo.ClientVersion != "" || withoutInfo.ProtocolVersion != "2024-11-05" {
		t.Fatalf("missing clientInfo parsed incorrectly: %+v", withoutInfo)
	}
	malformed := parseClientCapabilities(json.RawMessage(`"not-an-initialize-object"`), observed)
	if malformed.ClientName != "" || malformed.ProtocolVersion != "" || malformed.SamplingSupported || malformed.ObservedAt == "" {
		t.Fatalf("malformed payload did not fail closed: %+v", malformed)
	}
}

func TestClientCapabilityStoreMultipleInitializeAndSessionSeparation(t *testing.T) {
	store := newClientCapabilityStore()
	store.Record("http-a", json.RawMessage(`{"clientInfo":{"name":"first"},"capabilities":{"sampling":{}}}`), time.Unix(1, 0))
	store.Record("stdio", json.RawMessage(`{"clientInfo":{"name":"stdio"},"capabilities":{"roots":{}}}`), time.Unix(2, 0))
	store.Record("http-a", json.RawMessage(`{"clientInfo":{"name":"reconnected"},"capabilities":{}}`), time.Unix(3, 0))

	httpSnapshot := store.Snapshot("http-a")
	if httpSnapshot.ClientName != "reconnected" || httpSnapshot.SamplingSupported || httpSnapshot.ObservedAt != time.Unix(3, 0).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("reinitialize snapshot=%+v", httpSnapshot)
	}
	stdioSnapshot := store.Snapshot("stdio")
	if stdioSnapshot.ClientName != "stdio" || !stdioSnapshot.RootsSupported {
		t.Fatalf("stdio session overwritten: %+v", stdioSnapshot)
	}
}

func TestClientCapabilitiesSchemaIsExactAndStoresNoSensitiveContent(t *testing.T) {
	secret := "private-prompt-and-message-id"
	got := parseClientCapabilities(json.RawMessage(`{
		"protocolVersion":"2025-06-18",
		"clientInfo":{"name":"safe","version":"1","privateId":"`+secret+`"},
		"capabilities":{"sampling":{},"unknown":{"prompt":"`+secret+`"}},
		"messages":[{"content":"`+secret+`"}],
		"metadata":{"token":"`+secret+`"}
	}`), time.Unix(4, 0))
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("sensitive initialize content retained: %s", encoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != len(capabilityKeys) {
		t.Fatalf("field count=%d body=%s", len(fields), encoded)
	}
	for _, key := range capabilityKeys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing field %q in %s", key, encoded)
		}
	}
}

func TestMCPClientCapabilitiesToolUsesCurrentSession(t *testing.T) {
	server := stampServer(t)
	callSession(t, server, "http-a", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"http-client","version":"1"},"capabilities":{"sampling":{}}}}`)
	callSession(t, server, stdioSessionKey, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"clientInfo":{"name":"stdio-client","version":"2"},"capabilities":{"roots":{}}}}`)

	httpText := toolText(t, callSession(t, server, "http-a", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcp_client_capabilities","arguments":{}}}`))
	stdioText := toolText(t, callSession(t, server, stdioSessionKey, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mcp_client_capabilities","arguments":{}}}`))
	if !strings.Contains(httpText, `"client_name":"http-client"`) || !strings.Contains(httpText, `"sampling_supported":true`) || strings.Contains(httpText, "stdio-client") {
		t.Fatalf("http snapshot=%s", httpText)
	}
	if !strings.Contains(stdioText, `"client_name":"stdio-client"`) || !strings.Contains(stdioText, `"roots_supported":true`) || strings.Contains(stdioText, "http-client") {
		t.Fatalf("stdio snapshot=%s", stdioText)
	}
}

func callSession(t *testing.T, server *Server, sessionKey, raw string) rpcResponse {
	t.Helper()
	encoded := server.handleObservedSession([]byte(raw), observability.TransportHTTP, observability.NewRequestID(), sessionKey)
	var response rpcResponse
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("decode response: %v body=%s", err, encoded)
	}
	return response
}
