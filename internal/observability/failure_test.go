package observability

import (
	"errors"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("private writer failure text")
}

func TestWriterFailuresAreCountedWithoutEventData(t *testing.T) {
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: DefaultMaxBytes}, failingWriter{})
	if err != nil {
		t.Fatal(err)
	}
	err = logger.Emit(Event{
		Level:     LevelInfo,
		Component: ComponentServer,
		Name:      EventServerStop,
		Outcome:   OutcomeSuccess,
	})
	if err == nil {
		t.Fatal("expected writer failure")
	}
	if logger.Failures() != 1 {
		t.Fatalf("failures = %d", logger.Failures())
	}
}
