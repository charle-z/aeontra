package mcpserver

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestMalformedBatchEmitsClosedParseFailure(t *testing.T) {
	server, _, events := newObservedServer(t)
	requestID := observability.NewRequestID()
	responses := server.handleBatchObserved([]byte("[{"), observability.TransportHTTP, requestID)
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	decoded := decodeEvents(t, events.String())
	if len(decoded) != 1 {
		t.Fatalf("events = %+v", decoded)
	}
	event := decoded[0]
	if event.RequestID != requestID || event.Transport != observability.TransportHTTP {
		t.Fatalf("correlation mismatch: %+v", event)
	}
	if event.Method != observability.MethodOther || event.Outcome != observability.OutcomeError || event.ErrorClass != observability.ErrorParse {
		t.Fatalf("unexpected parse event: %+v", event)
	}
}
