package mcpserver

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/htbaction"
)

func TestHTBActionsRemainDefinedButAreAbsentFromExternalCatalog(t *testing.T) {
	server := stampServer(t)
	definitions := htbaction.Definitions()
	if len(definitions) != 6 {
		t.Fatalf("runtime definitions=%d", len(definitions))
	}
	for _, definition := range definitions {
		if _, exists := server.table[definition.Name]; exists {
			t.Fatalf("runtime-only HTB action %s is externally callable", definition.Name)
		}
		for _, name := range server.order {
			if name == definition.Name {
				t.Fatalf("runtime-only HTB action %s is externally listed", definition.Name)
			}
		}
	}
}

func TestExternalLabToolsDoNotAcceptCommandsCredentialsOrFlags(t *testing.T) {
	server := stampServer(t)
	for _, name := range []string{"workspace_lab_prepare", "workspace_lab_retarget", "workspace_autopilot_start", "workspace_autopilot_status", "workspace_autopilot_pause", "workspace_autopilot_resume", "workspace_autopilot_cancel"} {
		entry, ok := server.table[name]
		if !ok || entry.def.InputSchema["additionalProperties"] != false {
			t.Fatalf("closed lab tool missing: %s", name)
		}
		properties := entry.def.InputSchema["properties"].(map[string]any)
		for _, forbidden := range []string{"command", "flags", "password", "credential", "script", "url", "path"} {
			if _, ok := properties[forbidden]; ok {
				t.Fatalf("%s exposes %s", name, forbidden)
			}
		}
	}
}
