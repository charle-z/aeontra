package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grype.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRequiresReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--report") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPassesWithoutHighFindings(t *testing.T) {
	report := writeReport(t, `{"matches":[{"vulnerability":{"id":"CVE-LOW","severity":"Low","fix":{"versions":[]}},"artifact":{"name":"pkg","version":"1","type":"apk","locations":[]}}]}`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--report", report, "--minimum", "high"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") || !strings.Contains(stdout.String(), "High") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunEmitsAnnotationsAndFailsForHighFindings(t *testing.T) {
	report := writeReport(t, `{"matches":[{"vulnerability":{"id":"CVE-HIGH","severity":"High","fix":{"versions":["2.0"]}},"artifact":{"name":"libdemo","version":"1.0","type":"apk","locations":[{"path":"/lib/libdemo.so"}]}}]}`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--report", report, "--minimum", "high", "--annotation-file", "Dockerfile"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, required := range []string{"::error file=Dockerfile", "CVE-HIGH", "package=libdemo", "fixed=2.0"} {
		if !strings.Contains(stdout.String(), required) {
			t.Fatalf("stdout %q does not contain %q", stdout.String(), required)
		}
	}
	if !strings.Contains(stderr.String(), "1 finding") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunRejectsInvalidMinimumAndMalformedReport(t *testing.T) {
	for name, args := range map[string][]string{
		"severity": {"--report", "missing.json", "--minimum", "extreme"},
		"report":   {"--report", writeReport(t, `{`), "--minimum", "high"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code == 0 {
				t.Fatalf("run unexpectedly passed stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}
