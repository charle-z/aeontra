package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeRepositoryWriteService struct {
	calls []string
}

func (f *fakeRepositoryWriteService) ApplyPatchIn(repo, patch string, approve bool) (string, error) {
	f.calls = append(f.calls, "patch:"+repo+":"+patch)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "patch-result", nil
}

func (f *fakeRepositoryWriteService) CreateFileIn(repo, path, content string, approve bool) (string, error) {
	f.calls = append(f.calls, "create:"+repo+":"+path+":"+content)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "create-result", nil
}

func TestRegisterRepositoryWritesDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeRepositoryWriteService{}
	var registered []Tool
	RegisterRepositoryWrites(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q, want 1", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"apply_patch", "create_file"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"apply_patch": "Apply a unified diff (patch-first). Optional repo makes patch paths relative to that jailed repo. Validated with 'git apply --check' first; targets jailed and secret-protected. In ask mode, set approve=true to apply after review.",
		"create_file": "Create a NEW file (patch-first: built as a diff and validated; refuses to overwrite — use apply_patch to modify). Jailed and secret-protected. In ask mode set approve=true.",
	}
	for _, tool := range registered {
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed: %q", tool.Name, tool.Description)
		}
	}

	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
	}
	wantSchemas := map[string]map[string]any{
		"apply_patch": object(map[string]any{
			"patch":   strProp("unified diff text"),
			"approve": boolProp("apply even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "patch"),
		"create_file": object(map[string]any{
			"path":    strProp("new file path relative to the project root"),
			"content": strProp("file content"),
			"approve": boolProp("create even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "path", "content"),
	}
	for name, wantSchema := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, wantSchema) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, wantSchema)
		}
	}

	calls := []struct {
		name string
		args string
		want string
	}{
		{name: "apply_patch", args: `{"repo":"project","patch":"diff-body","approve":true}`, want: "patch-result"},
		{name: "create_file", args: `{"repo":"project","path":"new.txt","content":"body","approve":true}`, want: "create-result"},
	}
	for _, call := range calls {
		result, err := byName[call.name].Handler(json.RawMessage(call.args))
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if result != call.want {
			t.Fatalf("%s result = %q, want %q", call.name, result, call.want)
		}
	}

	wantCalls := []string{
		"patch:project:diff-body", "approved",
		"create:project:new.txt:body", "approved",
	}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("service calls = %#v, want %#v", service.calls, wantCalls)
	}
}
