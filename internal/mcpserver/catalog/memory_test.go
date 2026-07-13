package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeMemoryService struct {
	calls []string
}

func (f *fakeMemoryService) MemoryReadIn(repo string) (string, error) {
	f.calls = append(f.calls, "read:"+repo)
	return "read-result", nil
}

func (f *fakeMemoryService) MemoryWriteIn(repo, section, content string, approve bool) (string, error) {
	f.calls = append(f.calls, "write:"+repo+":"+section+":"+content)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "write-result", nil
}

func TestRegisterMemoryDefinesStableOrderAndRoutesHandlers(t *testing.T) {
	service := &fakeMemoryService{}
	var registered []Tool
	RegisterMemory(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"memory_read", "memory_write"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}
	result, err := byName["memory_read"].Handler(json.RawMessage(`{"repo":"/repos/demo"}`))
	if err != nil || result != "read-result" {
		t.Fatalf("memory_read result=%q err=%v", result, err)
	}
	result, err = byName["memory_write"].Handler(json.RawMessage(`{"repo":"/repos/demo","section":"plan","content":"body","approve":true}`))
	if err != nil || result != "write-result" {
		t.Fatalf("memory_write result=%q err=%v", result, err)
	}
	wantCalls := []string{"read:/repos/demo", "write:/repos/demo:plan:body", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}

func TestRegisterMemoryPreservesRequiredFields(t *testing.T) {
	var registered []Tool
	RegisterMemory(func(tool Tool) {
		registered = append(registered, tool)
	}, &fakeMemoryService{})
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}
	if required, ok := byName["memory_read"].InputSchema["required"]; ok {
		t.Fatalf("memory_read unexpectedly requires fields: %#v", required)
	}
	want := []string{"section", "content"}
	got, _ := byName["memory_write"].InputSchema["required"].([]string)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("memory_write required = %#v, want %#v", got, want)
	}
}
