package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestPromptlessWorkspaceContinuationDocumentationIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}
	document := read("workspace-runtime-continuation.md")
	for _, required := range []string{
		"workspace_runtime_continue",
		`"workspace_id"`,
		`"timeout_seconds"`,
		"resume-local-contract-v1",
		".mcp-devbox/instructions.md",
		".mcp-devbox/current-state.md",
		"one runtime",
		"never retried automatically",
		"lab init",
		"signed bootstrap",
	} {
		if !strings.Contains(strings.ToLower(document), strings.ToLower(required)) {
			t.Errorf("continuation documentation missing %q", required)
		}
	}
	serverSource := read("../internal/mcpserver/workspace_runtime_continue.go")
	for _, required := range []string{
		`workspaceContinuationGoalVersion = "resume-local-contract-v1"`,
		`Name:        "workspace_runtime_continue"`,
		`"workspace_id"`,
		`"timeout_seconds"`,
		`"idempotency_key"`,
		`[]string{"workspace_id", "timeout_seconds", "idempotency_key"}`,
	} {
		if !strings.Contains(serverSource, required) {
			t.Errorf("continuation implementation missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`"objective":`, `"prompt":`, `"instructions":`, `"command":`, `"target":`,
		`"host":`, `"ip":`, `"credential":`, `"flag":`, `"options":`,
	} {
		if strings.Contains(serverSource, forbidden) {
			t.Errorf("public continuation schema contains forbidden field %q", forbidden)
		}
	}
	onboarding := read("install-opencode-edge-parrot.md")
	for _, required := range []string{"Continue a registered workspace without a prompt", "workspace_runtime_continue", "resume-local-contract-v1"} {
		if !strings.Contains(onboarding, required) {
			t.Errorf("Parrot onboarding missing %q", required)
		}
	}
}
