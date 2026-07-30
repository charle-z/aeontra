package docs

import (
	"os"
	"strings"
	"testing"
)

func TestStableMCPFrontDoorDocumentationIsCanonicalAndLinked(t *testing.T) {
	t.Parallel()
	frontDoor, err := os.ReadFile("stable-mcp-front-door.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(frontDoor)
	for _, required := range []string{
		"MCP_FRONT_DOOR_BACKEND_URL",
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL",
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH",
		"/front-door/healthz",
		"Requests already accepted continue",
		"cannot prevent a ChatGPT client-side",
		"platform_front_door_create_preview",
		"platform_front_door_create",
		"platform_front_door_status",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("front-door documentation missing %q", required)
		}
	}
	configuration, err := os.ReadFile("configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, variable := range []string{
		"MCP_FRONT_DOOR_BACKEND_URL",
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL",
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH",
	} {
		if !strings.Contains(string(configuration), variable) {
			t.Fatalf("canonical configuration missing %q", variable)
		}
	}
	mapping, err := os.ReadFile("documentation-map.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mapping), "stable-mcp-front-door.md") {
		t.Fatal("documentation map does not link the stable front door")
	}
}
