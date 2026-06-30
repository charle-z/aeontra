package main

import "testing"

func TestEnvFallback(t *testing.T) {
	// Flag value wins over env.
	t.Setenv("MCP_DEVBOX_TEST_CMD", "from-env")
	if got := envFallback("from-flag", "MCP_DEVBOX_TEST_CMD"); got != "from-flag" {
		t.Errorf("flag should win, got %q", got)
	}
	// Empty flag falls back to env.
	if got := envFallback("", "MCP_DEVBOX_TEST_CMD"); got != "from-env" {
		t.Errorf("empty flag should use env, got %q", got)
	}
	// Whitespace-only flag also falls back to env.
	if got := envFallback("   ", "MCP_DEVBOX_TEST_CMD"); got != "from-env" {
		t.Errorf("blank flag should use env, got %q", got)
	}
	// Both empty -> empty (secure default downstream).
	if got := envFallback("", "MCP_DEVBOX_UNSET_VAR_XYZ"); got != "" {
		t.Errorf("both empty should be empty, got %q", got)
	}
}
