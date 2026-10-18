package observability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEmitOwnsSchemaVersionAndTimestamp(t *testing.T) {
	var output bytes.Buffer
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}, &output)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 7, 13, 20, 0, 0, 123, time.UTC)
	logger.now = func() time.Time { return fixed }
	secretTime := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	if err := logger.Emit(Event{
		SchemaVersion: 999,
		Time:          secretTime,
		Level:         LevelInfo,
		Component:     ComponentServer,
		Name:          EventServerStart,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secretTime) {
		t.Fatalf("caller-controlled time leaked: %s", output.String())
	}
	var event Event
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d", event.SchemaVersion)
	}
	if event.Time != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("time = %q", event.Time)
	}
}
