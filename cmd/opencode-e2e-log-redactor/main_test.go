package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactRetainsOnlyBoundedSafeSignals(t *testing.T) {
	input := strings.Join([]string{
		`controller failed: model turn driver operation failed: response_cas /home/private/repo SQL SELECT prompt payload body command`,
		`Authorization: Bearer token-value`,
		`secret prompt body /home/private/repo`,
		`--- FAIL: TestThing /home/private/repo "secret"`,
		`permission denied: /private/path`,
		`OpenCode failed: slice_code=restart_permission_open /private/path prompt payload`,
		`mcp-edge failed: edge_failure=opencode_permission_connect /private/socket secret`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := redact(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, required := range []string{
		"E2E safe_driver_code=response_cas",
		"--- FAIL: TestThing <path> \"<redacted>\"",
		"E2E category=permission",
		"E2E safe_failure_code=restart_permission_open",
		"E2E safe_failure_code=opencode_permission_connect",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("redacted output omitted %q: %q", required, text)
		}
	}
	for _, forbidden := range []string{
		"Authorization", "token-value", "/home/private", "/private/path", "SQL", "SELECT", "prompt", "payload", "body", "command", "secret",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("redacted output leaked %q: %q", forbidden, text)
		}
	}
}

func TestRedactBoundsOutput(t *testing.T) {
	input := strings.Repeat("fatal error /private/path\n", maxLines+100)
	var output bytes.Buffer
	if err := redact(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(output.String(), "\n"); lines > maxLines {
		t.Fatalf("lines=%d max=%d", lines, maxLines)
	}
	if output.Len() > maxBytes {
		t.Fatalf("bytes=%d max=%d", output.Len(), maxBytes)
	}
}

func TestNormalizeDropsUnknownContent(t *testing.T) {
	for _, input := range []string{
		"Authorization: Bearer token",
		"prompt body payload /private/path",
		"cookie=session-value",
	} {
		if got := normalize(input); got != "" {
			t.Fatalf("input=%q normalized=%q", input, got)
		}
	}
}
