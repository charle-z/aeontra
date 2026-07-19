package mcpserver

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/htbaction"
)

func TestHTBActionCatalogIsExplicitClosedAndTransparent(t *testing.T) {
	server := stampServer(t)
	definitions := htbaction.Definitions()
	if len(definitions) != 6 {
		t.Fatalf("definitions=%d", len(definitions))
	}
	for _, definition := range definitions {
		entry, exists := server.table[definition.Name]
		if !exists || entry.handler == nil || entry.def.Version != "1" {
			t.Fatalf("tool %s is not registered", definition.Name)
		}
		if entry.def.InputSchema["additionalProperties"] != false {
			t.Fatalf("tool %s schema is not closed", definition.Name)
		}
		properties := entry.def.InputSchema["properties"].(map[string]any)
		if _, exists := properties["workspace_id"]; !exists {
			t.Fatalf("tool %s does not require workspace_id", definition.Name)
		}
		for _, forbidden := range []string{"target", "host", "ip", "password", "private_key", "key_path", "port", "pty"} {
			if _, exists := properties[forbidden]; exists {
				t.Fatalf("tool %s exposes %s", definition.Name, forbidden)
			}
		}
		description := strings.ToLower(entry.def.Description)
		if !strings.Contains(description, "hack the box") && !strings.Contains(description, "htb") && !strings.Contains(description, "ctf") {
			t.Fatalf("tool %s description hides its purpose: %s", definition.Name, entry.def.Description)
		}
		wantOpenWorld := definition.Name != htbaction.ToolStatus && definition.Name != htbaction.ToolSessionClose
		if entry.def.Annotations["openWorldHint"] != wantOpenWorld || entry.def.Annotations["readOnlyHint"] != (definition.Name == htbaction.ToolStatus) {
			t.Fatalf("tool %s annotations=%v", definition.Name, entry.def.Annotations)
		}
	}
}

func TestHTBActionPublicBoundaryRequiresAuthorizedRuntimeWorkspace(t *testing.T) {
	server := stampServer(t)
	arguments := `{"workspace_id":"` + testWorkspaceID + `"}`
	if got := callToolErrorText(t, server, htbaction.ToolStatus, arguments); got != errWorkspaceRegistryUnavailable.Error() {
		t.Fatalf("missing registry error=%q", got)
	}

	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "dev"))
	if got := callToolErrorText(t, server, htbaction.ToolStatus, arguments); !strings.Contains(got, "not an authorized htb-linux") {
		t.Fatalf("dev workspace accepted: %q", got)
	}

	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "htb-linux"))
	if got := callToolErrorText(t, server, htbaction.ToolStatus, arguments); got != errHTBActionRuntimeRequired.Error() {
		t.Fatalf("runtime boundary error=%q", got)
	}

	store := continuationStore(testWorkspaceID, "linux-workcell", "htb-linux")
	store.devices[testEdgeDeviceID] = false
	server.WithEdgeStore(store)
	if got := callToolErrorText(t, server, htbaction.ToolStatus, arguments); !strings.Contains(got, "active workspace not found") {
		t.Fatalf("inactive edge accepted: %q", got)
	}
}

func TestHTBActionSchemasRejectCallerSelectedTargetsCredentialsAndUnknownFields(t *testing.T) {
	server := stampServer(t)
	server.WithEdgeStore(continuationStore(testWorkspaceID, "linux-workcell", "htb-linux"))
	for _, field := range []string{"target", "host", "ip", "password", "private_key", "key_path", "objective"} {
		body := `{"workspace_id":"` + testWorkspaceID + `","` + field + `":"caller-controlled-sensitive-value"}`
		got := callToolErrorText(t, server, htbaction.ToolStatus, body)
		if !strings.Contains(got, "unknown field") || strings.Contains(got, "caller-controlled-sensitive-value") {
			t.Fatalf("field=%s error=%q", field, got)
		}
	}
}

func TestHTBActionCatalogOrderIsDeliberate(t *testing.T) {
	server := stampServer(t)
	positions := make(map[string]int)
	for index, name := range server.order {
		positions[name] = index
	}
	got := make([]int, 0, len(htbaction.Definitions()))
	for _, definition := range htbaction.Definitions() {
		got = append(got, positions[definition.Name])
	}
	want := append([]int(nil), got...)
	sort.Ints(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HTB tool order=%v", got)
	}
}
