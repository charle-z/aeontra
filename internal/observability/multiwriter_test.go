package observability

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestOneWriterFailureDoesNotPreventOtherWriter(t *testing.T) {
	var healthy bytes.Buffer
	logger := &Logger{
		writers: []io.Writer{failingWriter{}, &healthy},
		now:     time.Now,
	}
	err := logger.Emit(Event{
		Level:     LevelInfo,
		Component: ComponentServer,
		Name:      EventServerStop,
		Outcome:   OutcomeError,
	})
	if err == nil {
		t.Fatal("expected joined writer failure")
	}
	if logger.Failures() != 1 {
		t.Fatalf("failures = %d", logger.Failures())
	}
	if !strings.Contains(healthy.String(), `"event":"server_stop"`) {
		t.Fatalf("healthy writer did not receive event: %q", healthy.String())
	}
}
