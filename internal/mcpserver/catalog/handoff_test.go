package catalog

import (
	"encoding/json"
	"testing"
)

type fakeHandoffService struct {
	content string
}

func (f *fakeHandoffService) MemoryUpdateHandoff(content string) (string, error) {
	f.content = content
	return "handoff-result", nil
}

func TestRegisterHandoffDefinesStableContractAndRoutesHandler(t *testing.T) {
	service := &fakeHandoffService{}
	var registered []Tool
	RegisterHandoff(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "memory_update_handoff" || tool.Version != "1" {
		t.Fatalf("unexpected tool identity: %#v", tool)
	}
	if tool.Description != "Write a handoff note into .agent-memory/handoffs/ so any agent can resume. Denied in read-only mode; content redacted." {
		t.Fatalf("description changed: %q", tool.Description)
	}
	result, err := tool.Handler(json.RawMessage(`{"content":"resume here"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "handoff-result" || service.content != "resume here" {
		t.Fatalf("result=%q content=%q", result, service.content)
	}
	required, _ := tool.InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "content" {
		t.Fatalf("required = %#v", required)
	}
}
