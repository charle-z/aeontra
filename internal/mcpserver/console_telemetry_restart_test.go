package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/telemetry"
)

func TestConsoleSeparatesCurrentProcessFromDurableActivityAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry", "metrics.db")
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	store, err := telemetry.Open(telemetry.Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Observe(observability.Event{
		Name: observability.EventRPCRequest, Method: observability.MethodToolsCall,
		Transport: observability.TransportInternal, Outcome: observability.OutcomeSuccess,
		InputBytes: 400, OutputBytes: 200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := telemetry.Open(telemetry.Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	server, _, _ := newObservedServer(t)
	server.WithTelemetry(reopened)
	snapshot, err := server.consoleDataProvider("", nil, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Payload.RequestCount != 0 || snapshot.Payload.ToolCallCount != 0 || snapshot.Payload.InputBytes != 0 || snapshot.Payload.OutputBytes != 0 {
		t.Fatalf("new process counters were not reset: %+v", snapshot.Payload)
	}
	if snapshot.DurableActivity.Last24Hours.Requests != 1 || snapshot.DurableActivity.Last24Hours.ToolCalls != 1 || snapshot.DurableActivity.Last24Hours.EstimatedPayloadTokens != 150 {
		t.Fatalf("24h durable activity=%+v", snapshot.DurableActivity.Last24Hours)
	}
	if snapshot.DurableActivity.Lifetime.Requests != 1 || snapshot.DurableActivity.Lifetime.InputBytes != 400 || snapshot.DurableActivity.Lifetime.OutputBytes != 200 {
		t.Fatalf("lifetime=%+v", snapshot.DurableActivity.Lifetime)
	}
	if snapshot.Payload.Warning != "estimate, not provider billing" || snapshot.Payload.ProcessStartedAt == "" {
		t.Fatalf("process labeling=%+v", snapshot.Payload)
	}
}
