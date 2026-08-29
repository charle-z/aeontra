package docs_test

import (
	"strings"
	"testing"
)

func TestOpenAIClientBoundaryIsDocumented(t *testing.T) {
	for path, required := range map[string][]string{
		"../README.md": {
			"Supported integrations are client-initiated",
			"does not automate a client UI",
			"does not import consumer session cookies or tokens",
		},
		"analysis/codex-harness-compatibility.md": {
			"active authorized MCP client",
			"not browser automation",
			"never opens or drives the ChatGPT UI",
			"not a service for turning a consumer ChatGPT plan into a programmatic API",
		},
		"workspace-runtime-continuation.md": {
			"authorized MCP client such as ChatGPT",
			"the client drives each Edge request",
			"No daemon drives the client UI",
		},
		"security.md": {
			"no ChatGPT-specific browser preset",
			"consumer-session import",
			"rules of the target service",
		},
	} {
		content := readDoc(t, path)
		for _, marker := range required {
			if !containsNormalizedProse(content, marker) {
				t.Errorf("%s missing OpenAI client boundary %q", path, marker)
			}
		}
	}

	for path, content := range map[string]string{
		"analysis/codex-harness-compatibility.md": readDoc(t, "analysis/codex-harness-compatibility.md"),
		"workspace-runtime-continuation.md":       readDoc(t, "workspace-runtime-continuation.md"),
	} {
		for _, forbidden := range []string{
			"does not require model credits",
			"without giving it an OpenAI API key or a Codex subscription",
			"free tokens",
			"quota bypass",
		} {
			if strings.Contains(strings.ToLower(content), strings.ToLower(forbidden)) {
				t.Errorf("%s contains unsupported subscription or quota claim %q", path, forbidden)
			}
		}
	}
}
