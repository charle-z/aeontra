package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRequiresProfile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--profile") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintsPassingPackageResults(t *testing.T) {
	profile := writeProfile(t, `mode: atomic
github.com/charle-z/mcp-devbox/internal/policy/a.go:1.1,2.1 10 1
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--profile", profile,
		"--min", "github.com/charle-z/mcp-devbox/internal/policy=80",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	for _, required := range []string{"PASS", "internal/policy", "100.0%", ">= 80.0%"} {
		if !strings.Contains(stdout.String(), required) {
			t.Fatalf("stdout %q does not contain %q", stdout.String(), required)
		}
	}
}

func TestRunFailsBelowThreshold(t *testing.T) {
	profile := writeProfile(t, `mode: atomic
github.com/charle-z/mcp-devbox/internal/policy/a.go:1.1,2.1 7 1
github.com/charle-z/mcp-devbox/internal/policy/b.go:1.1,2.1 3 0
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--profile", profile,
		"--min", "github.com/charle-z/mcp-devbox/internal/policy=80",
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "FAIL") || !strings.Contains(stderr.String(), "below minimum") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestParseThresholdRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "package", "=80", "package=nope", "package=-1", "package=101"} {
		if _, err := parseThreshold(input); err == nil {
			t.Errorf("parseThreshold(%q) succeeded", input)
		}
	}
}
