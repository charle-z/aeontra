//go:build opencode_e2e && !windows

package edgeclient

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRelayContainerConfigTranslatesVirtualPathsOnce(t *testing.T) {
	input := `{"permission":{"*":"allow","external_directory":"deny","webfetch":"deny","websearch":"deny"},"provider":{"bridge":{"npm":"file:///mcp-provider","options":{"socketPath":"/runtime/model-turn.sock"}}}}`
	translate := func(value string) string {
		switch {
		case value == "/mcp-provider":
			return "/workspace/integrations/opencode/provider"
		case value == "/runtime":
			return "/tmp/runtime"
		case strings.HasPrefix(value, "/runtime/"):
			return "/tmp/runtime/" + strings.TrimPrefix(value, "/runtime/")
		case value == "/workspace":
			return "/tmp/runtime-workspace"
		case strings.HasPrefix(value, "/workspace/"):
			return "/tmp/runtime-workspace/" + strings.TrimPrefix(value, "/workspace/")
		default:
			return value
		}
	}
	encoded, err := relayContainerConfig(input, translate)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(encoded), &config); err != nil {
		t.Fatal(err)
	}
	permission := config["permission"].(map[string]any)
	if permission["external_directory"] != "allow" || permission["webfetch"] != "deny" || permission["websearch"] != "deny" || permission["*"] != "allow" {
		t.Fatalf("unexpected relay permissions: %#v", permission)
	}
	bridge := config["provider"].(map[string]any)["bridge"].(map[string]any)
	if bridge["npm"] != "file:///workspace/integrations/opencode/provider" {
		t.Fatalf("provider path was translated more than once: %v", bridge["npm"])
	}
	options := bridge["options"].(map[string]any)
	if options["socketPath"] != "/tmp/runtime/model-turn.sock" {
		t.Fatalf("socket path was not translated exactly once: %v", options["socketPath"])
	}
}
