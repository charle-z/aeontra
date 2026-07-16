//go:build opencode_e2e && !windows

package edgeclient

import (
	"encoding/json"
	"testing"
)

func TestRelayContainerConfigChangesOnlyExternalDirectoryBoundary(t *testing.T) {
	input := `{"permission":{"*":"allow","external_directory":"deny","webfetch":"deny","websearch":"deny"},"provider":{"bridge":{"npm":"file:///provider"}}}`
	encoded, err := relayContainerConfig(input)
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
}
