package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeReport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grype.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAcceptsOnlyApprovedWorkcellRiskAndKeepsItVisible(t *testing.T) {
	dir := t.TempDir()
	definition := filepath.Join(dir, "Dockerfile.sandbox-workcell")
	content := []byte("FROM reviewed-workcell\n")
	if err := os.WriteFile(definition, content, 0o600); err != nil {
		t.Fatal(err)
	}
	imageID := "sha256:" + strings.Repeat("a", 64)
	report := writeReport(t, fmt.Sprintf(`{"source":{"type":"image","target":{"imageID":%q}},"matches":[{"vulnerability":{"id":"GHSA-ch52-4w7c-c8xp","severity":"High","fix":{"versions":[]}},"artifact":{"name":"http-cache-semantics","version":"4.2.0","type":"npm","locations":[{"path":"/usr/lib/node_modules/npm/node_modules/http-cache-semantics/package.json"}]}}]}`, imageID))
	policy := filepath.Join(dir, "risk.json")
	document := fmt.Sprintf(`{"schema_version":1,"vulnerability_ids":["CVE-2026-93748","GHSA-ch52-4w7c-c8xp"],"package":"http-cache-semantics","version":"4.2.0","type":"npm","location":"/usr/lib/node_modules/npm/node_modules/http-cache-semantics/package.json","image_definition":"Dockerfile.sandbox-workcell","image_definition_sha256":"%x","approved_at":"2026-10-03T13:24:00Z","expires_at":"2026-10-17T13:24:00Z","owner":"charle-z","reason":"Explicit temporary risk acceptance; not a remediation."}`, sha256.Sum256(content))
	if err := os.WriteFile(policy, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runAt([]string{"--report", report, "--minimum", "high", "--annotation-file", definition, "--accepted-risk", policy, "--image-id", imageID}, &stdout, &stderr, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "::warning") || !strings.Contains(stdout.String(), "accepted risk") || !strings.Contains(stdout.String(), "GHSA-ch52-4w7c-c8xp") {
		t.Fatalf("accepted risk must remain visible: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "PASS no vulnerabilities") {
		t.Fatalf("must not claim no vulnerabilities: %s", stdout.String())
	}
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
