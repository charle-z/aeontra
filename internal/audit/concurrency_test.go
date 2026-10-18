package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestLoggerConcurrentEntriesRemainCompleteAndValid(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output)
	const workers = 128
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := logger.Log(Entry{
				Tool:     fmt.Sprintf("tool-%03d", index),
				Decision: Allow,
				Args:     "index=" + fmt.Sprint(index),
				Files:    []string{"/repo/" + secret + fmt.Sprintf("/%03d", index)},
			}); err != nil {
				t.Errorf("log %d: %v", index, err)
			}
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != workers {
		t.Fatalf("audit lines = %d, want %d", len(lines), workers)
	}
	seen := make(map[string]bool, workers)
	for _, line := range lines {
		if strings.Contains(line, secret) {
			t.Fatalf("secret leaked in concurrent audit line: %s", line)
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSONL entry: %v: %s", err, line)
		}
		if entry.Tool == "" || seen[entry.Tool] {
			t.Fatalf("missing or duplicate audit tool: %q", entry.Tool)
		}
		seen[entry.Tool] = true
	}
}
