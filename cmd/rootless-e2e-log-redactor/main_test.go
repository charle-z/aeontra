package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactKeepsOnlyBoundedAllowlistedSignals(t *testing.T) {
	input := strings.Join([]string{
		"=== RUN   TestTrustedLinuxWorkcellRootlessE2E",
		"rootless command failed: exit status 125 output=permission denied /home/private Authorization: Bearer token-value cookie=session body=secret",
		"podman-compose failed: connection refused at /run/user/1001/podman.sock",
		"P12 rootless category=stage_chromium",
		"Chromium smoke failed: private body",
		"--- FAIL: TestTrustedLinuxWorkcellRootlessE2E (1.23s)",
		"FAIL",
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := redact(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, required := range []string{
		"=== RUN   TestTrustedLinuxWorkcellRootlessE2E",
		"P12 rootless category=permission",
		"P12 rootless category=compose",
		"P12 rootless category=stage_chromium",
		"P12 rootless category=chromium",
		"--- FAIL: TestTrustedLinuxWorkcellRootlessE2E (1.23s)",
		"FAIL",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("redacted output omitted %q: %q", required, text)
		}
	}
	for _, forbidden := range []string{
		"Authorization", "token-value", "cookie", "session", "secret", "body", "/home/", "/run/user/", "1001", "output=",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("redacted output leaked %q: %q", forbidden, text)
		}
	}
}

func TestNormalizeKeepsClosedPostgreSQLOperationCategories(t *testing.T) {
	for _, operation := range []string{"image_inspect", "network_create", "volume_create", "container_run", "readiness_query"} {
		input := "P12 rootless category=postgres_" + operation
		if got := normalize(input); got != input {
			t.Fatalf("input=%q normalized=%q", input, got)
		}
	}
	for _, forbidden := range []string{"shell", "private_path", "arbitrary"} {
		if got := normalize("P12 rootless category=postgres_" + forbidden); got != "" {
			t.Fatalf("unsafe operation %q normalized=%q", forbidden, got)
		}
	}
}

func TestNormalizeDropsUnknownAndSensitiveContent(t *testing.T) {
	for _, input := range []string{
		"Authorization: Bearer token",
		"cookie=session-value",
		"private body /home/private/repo",
		"=== RUN   TestUnrelated",
		"P12 rootless category=stage_private_path",
	} {
		if got := normalize(input); got != "" {
			t.Fatalf("input=%q normalized=%q", input, got)
		}
	}
}

func TestRedactBoundsOutput(t *testing.T) {
	input := strings.Repeat("permission denied at /private/path\n", maxLines+100)
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

func TestNormalizeClassifiesRootlessLifecycleFailures(t *testing.T) {
	tests := map[string]string{
		"rootless command failed: exit status 41 output=private":              "P12 rootless category=workspace_fixture_missing",
		"rootless command failed: exit status 42 output=private":              "P12 rootless category=workspace_bind_writable",
		"rootless command failed: exit status 125 output=private":             "P12 rootless category=engine_command",
		"rootless command failed: exit status 126 output=private":             "P12 rootless category=container_command_unexecutable",
		"rootless command failed: exit status 127 output=private":             "P12 rootless category=container_command_not_found",
		"rootful container socket is accessible to the rootless test user": "P12 rootless category=rootful_socket",
		"rootless container socket is unsafe":                              "P12 rootless category=endpoint_socket",
		"rootless pod resources inherited by runtime cycle":                "P12 rootless category=inherited_state",
		"PostgreSQL healthcheck timed out":                                 "P12 rootless category=postgresql_health",
		"podman-compose cleanup failed":                                    "P12 rootless category=compose_cleanup",
		"Error: no such network p12-net-deadbeef":                          "P12 rootless category=postgres_network",
		"Error: no such image localhost/p12-postgres-fixture:17-alpine":    "P12 rootless category=postgres_image",
		"rootless pod inventory failed":                                    "P12 rootless category=pod_inventory",
		"rootless container inventory failed":                              "P12 rootless category=container_inventory",
		"rootless network cleanup failed":                                  "P12 rootless category=network_cleanup",
		"rootless volume cleanup failed":                                   "P12 rootless category=volume_cleanup",
		"rootless container engine returned an unsafe resource identifier": "P12 rootless category=unsafe_resource_id",
	}
	for input, expected := range tests {
		if got := normalize(input); got != expected {
			t.Fatalf("input=%q normalized=%q expected=%q", input, got, expected)
		}
	}
}

func TestNormalizeKeepsCleanupTestBoundary(t *testing.T) {
	for _, input := range []string{
		"=== RUN   TestTrustedLinuxWorkcellRootlessCleanupE2E",
		"--- PASS: TestTrustedLinuxWorkcellRootlessCleanupE2E (0.12s)",
		"--- FAIL: TestTrustedLinuxWorkcellRootlessCleanupE2E (1.23s)",
	} {
		if got := normalize(input); got != input {
			t.Fatalf("input=%q normalized=%q", input, got)
		}
	}
}
