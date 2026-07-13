package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRegisterRuntimeDefinesStableToolContract(t *testing.T) {
	var registered []Tool
	RegisterRuntime(func(tool Tool) {
		registered = append(registered, tool)
	}, func() (any, error) {
		return map[string]any{"status": "ok"}, nil
	})

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "system_runtime_info" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Description != "Return the live non-sensitive server version, commit, protocol version, tool count, and deterministic catalog hash." {
		t.Fatalf("description changed: %q", tool.Description)
	}
	wantSchema := map[string]any{"type": "object", "properties": map[string]any{}}
	if !reflect.DeepEqual(tool.InputSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", tool.InputSchema, wantSchema)
	}
	if tool.Version != "1" {
		t.Fatalf("version = %q, want 1", tool.Version)
	}
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"status":"ok"}` {
		t.Fatalf("handler result = %q", result)
	}
}
