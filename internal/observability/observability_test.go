package observability

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestDefaultConfigUsesStderrAndBoundedSize(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeStderr {
		t.Fatalf("mode = %q", cfg.Mode)
	}
	if cfg.MaxBytes != 16<<20 {
		t.Fatalf("max bytes = %d", cfg.MaxBytes)
	}
}

func TestEventSchemaHasNoFreeFormOrSensitiveFields(t *testing.T) {
	typeOfEvent := reflect.TypeOf(Event{})
	forbidden := []string{
		"prompt", "body", "params", "arguments", "response", "result", "content",
		"message", "error", "path", "file", "repo", "command", "argv", "target",
		"host", "domain", "ip", "url", "query", "header", "token", "identity", "user",
	}
	for i := 0; i < typeOfEvent.NumField(); i++ {
		field := typeOfEvent.Field(i)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, banned := range forbidden {
			if banned == "error" && strings.Contains(name, "error_class") {
				continue
			}
			if strings.Contains(name, banned) {
				t.Fatalf("event field %s violates forbidden field %q", field.Name, banned)
			}
		}
		if field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface {
			t.Fatalf("event field %s permits arbitrary values", field.Name)
		}
	}
}

func TestEmitWritesOneJSONLineAndDefensivelyRedactsLabels(t *testing.T) {
	var output bytes.Buffer
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: 16 << 20}, &output)
	if err != nil {
		t.Fatal(err)
	}
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	if err := logger.Emit(Event{
		Level:      LevelInfo,
		Component:  ComponentMCP,
		Name:       EventRPCRequest,
		RequestID:  NewRequestID(),
		Transport:  TransportHTTP,
		Method:     MethodToolsCall,
		Tool:       secret,
		Outcome:    OutcomeError,
		ErrorClass: ErrorTool,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("not one JSONL record: %q", output.String())
	}
	if strings.Contains(output.String(), secret) {
		t.Fatalf("secret leaked: %s", output.String())
	}
	var event Event
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.SchemaVersion != 1 || event.Time == "" || event.Name != EventRPCRequest {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.Tool != "redacted" {
		t.Fatalf("tool label = %q", event.Tool)
	}
}

func TestConcurrentEventsRemainLineSafe(t *testing.T) {
	var output bytes.Buffer
	logger, err := Open(Config{Mode: ModeStderr, MaxBytes: 16 << 20}, &output)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = logger.Emit(Event{Level: LevelInfo, Component: ComponentMCP, Name: EventRPCRequest, Outcome: OutcomeSuccess})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 100 {
		t.Fatalf("lines = %d", len(lines))
	}
	for _, line := range lines {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("corrupt line: %q: %v", line, err)
		}
	}
}

func TestFileModeUsesPrivatePermissionsAndFourFixedSegments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "observability.jsonl")
	logger, err := Open(Config{Mode: ModeFile, Path: path, MaxBytes: MinMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	rotating, ok := logger.writers[0].(*rotatingFile)
	if !ok || rotating.segments != DefaultSegments {
		t.Fatalf("file mode segments = %#v", logger.writers[0])
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenRotatingWriter(path, 1024, DefaultSegments)
	if err != nil {
		t.Fatal(err)
	}
	large := []byte(strings.Repeat("a", 900) + "\n")
	for i := 0; i < 100; i++ {
		if _, err := writer.Write(large); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + ".1", path + ".2", path + ".3"} {
		info, err := os.Stat(candidate)
		if err != nil {
			t.Fatalf("stat %s: %v", candidate, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", candidate, info.Mode().Perm())
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf("unexpected fifth segment: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o", dirInfo.Mode().Perm())
	}
}

func TestConfigValidationFailsClosed(t *testing.T) {
	for _, cfg := range []Config{
		{Mode: "network", MaxBytes: 16 << 20},
		{Mode: ModeFile, Path: "relative.jsonl", MaxBytes: 16 << 20},
		{Mode: ModeFile, Path: filepath.Join(t.TempDir(), "x.jsonl"), MaxBytes: MinMaxBytes - 1},
		{Mode: ModeStderr, MaxBytes: MaxMaxBytes + 1},
	} {
		if _, err := Open(cfg, &bytes.Buffer{}); err == nil {
			t.Fatalf("config should fail: %+v", cfg)
		}
	}
}

func TestDisabledLoggerIsNoop(t *testing.T) {
	logger, err := Open(Config{Mode: ModeOff, MaxBytes: 16 << 20}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Emit(Event{Level: LevelInfo, Component: ComponentServer, Name: EventServerStart}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

type captureSink struct{ events []Event }

func (s *captureSink) Observe(event Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestSinkReceivesOnlyNormalizedClosedDimensions(t *testing.T) {
	logger, err := Open(Config{Mode: ModeOff, MaxBytes: DefaultMaxBytes}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	sink := &captureSink{}
	logger.WithSink(sink)
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	if err := logger.Emit(Event{Transport: Transport(secret), Tool: secret, ProjectID: secret, TaskID: secret, Outcome: Outcome(secret), InputBytes: -1}); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("sink events = %d", len(sink.events))
	}
	got := sink.events[0]
	if got.Transport != TransportOther || got.Tool != "redacted" || got.ProjectID != "redacted" || got.TaskID != "redacted" || got.Outcome != OutcomeOther || got.InputBytes != 0 {
		t.Fatalf("unnormalized sink event: %+v", got)
	}
}
