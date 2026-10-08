package mcpserver

import "testing"

func TestProjectNetworkToolsExposeOnlyStructuredVPNProbes(t *testing.T) {
	server := stampServer(t)
	for _, name := range []string{"project_network_route", "project_network_probe"} {
		entry, ok := server.table[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if entry.def.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s schema is not closed", name)
		}
		properties := entry.def.InputSchema["properties"].(map[string]any)
		for _, required := range []string{"alias", "target", "destination"} {
			if _, ok := properties[required]; !ok {
				t.Fatalf("%s missing %s", name, required)
			}
		}
		for _, forbidden := range []string{"argv", "command", "script", "stdin", "environment", "url", "path", "password", "credential"} {
			if _, ok := properties[forbidden]; ok {
				t.Fatalf("%s exposes executable or credential field %s", name, forbidden)
			}
		}
		annotations := entry.def.Annotations
		if annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false || annotations["openWorldHint"] != true {
			t.Fatalf("%s annotations=%v", name, annotations)
		}
	}

	route := server.table["project_network_route"].def.InputSchema["properties"].(map[string]any)
	if _, ok := route["ports"]; ok {
		t.Fatal("route accepts ports")
	}
	if _, ok := route["timeout_ms"]; ok {
		t.Fatal("route accepts timeout")
	}

	probe := server.table["project_network_probe"].def.InputSchema["properties"].(map[string]any)
	ports := probe["ports"].(map[string]any)
	if ports["minItems"] != 1 || ports["maxItems"] != 64 {
		t.Fatalf("ports bounds=%v", ports)
	}
	timeout := probe["timeout_ms"].(map[string]any)
	if timeout["minimum"] != 50 || timeout["maximum"] != 1500 {
		t.Fatalf("timeout bounds=%v", timeout)
	}
}
