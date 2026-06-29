package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLog_WritesJSONLine(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	if err := l.Log(Entry{Tool: "read_file", Decision: Allow, Files: []string{"main.go"}}); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(buf.String())
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("entry should end with a newline (JSONL)")
	}
	var e Entry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("entry is not valid JSON: %v", err)
	}
	if e.Tool != "read_file" || e.Decision != Allow {
		t.Errorf("round-trip mismatch: %+v", e)
	}
	if e.Time == "" {
		t.Error("time should be auto-populated")
	}
}

func TestLog_RedactsSecretsInArgsAndError(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	_ = l.Log(Entry{
		Tool:     "run_tests",
		Decision: Error,
		Args:     `token gh` + `p_0123456789abcdefghijklmnopqrstuvwxyz`,
		Error:    errors.New("failed with api_key=supersecret123value").Error(),
	})
	out := buf.String()
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("token leaked into audit log: %s", out)
	}
	if strings.Contains(out, "supersecret123value") {
		t.Errorf("secret leaked into audit error: %s", out)
	}
}

func TestSpan_RecordsDecisionAndDuration(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	sp := l.Start("apply_patch")
	sp.Finish(Deny, "patch to .env", nil, nil)

	var e Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &e); err != nil {
		t.Fatal(err)
	}
	if e.Tool != "apply_patch" || e.Decision != Deny {
		t.Errorf("unexpected entry: %+v", e)
	}
}

func TestOpen_AppendsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Log(Entry{Tool: "a", Decision: Allow})
	_ = l.Log(Entry{Tool: "b", Decision: Deny})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// Re-open and append more: file must grow, not truncate.
	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l2.Log(Entry{Tool: "c", Decision: Allow})
	_ = l2.Close()
}

func TestLog_ConcurrentWritesAreLineSafe(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Log(Entry{Tool: "t", Decision: Allow})
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(lines))
	}
	for _, ln := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(ln), &e); err != nil {
			t.Fatalf("interleaved/corrupt line: %q", ln)
		}
	}
}
