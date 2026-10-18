package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogRedactsSecretsInFilePaths(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	path := "/repos/project/artifacts/" + secret + "/report.log"

	if err := logger.Log(Entry{
		Tool:     "read_file",
		Decision: Allow,
		Files:    []string{path, "safe/main.go"},
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("secret-bearing path leaked into audit log: %s", buf.String())
	}

	var entry Entry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.Files) != 2 {
		t.Fatalf("files = %#v", entry.Files)
	}
	if !strings.Contains(entry.Files[0], "***REDACTED-SECRET***") {
		t.Fatalf("secret path was not redacted: %#v", entry.Files)
	}
	if entry.Files[1] != "safe/main.go" {
		t.Fatalf("safe path changed: %#v", entry.Files)
	}
}
