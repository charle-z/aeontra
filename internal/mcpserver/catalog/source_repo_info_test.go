package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeSourceRepoInfoService struct {
	name string
}

func (f *fakeSourceRepoInfoService) SourceRepoInfo(name string) (string, error) {
	f.name = name
	return "info-result", nil
}

func TestRegisterSourceRepoInfoDefinesStableContractAndRoutesHandler(t *testing.T) {
	service := &fakeSourceRepoInfoService{}
	var registered []Tool
	RegisterSourceRepoInfo(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "github_repo_info" || tool.Version != "1" {
		t.Fatalf("tool = %s v%s", tool.Name, tool.Version)
	}
	wantDescription := "Read basic metadata for a repository under the configured GitHub owner. Token is never exposed and output is redacted."
	if tool.Description != wantDescription {
		t.Fatalf("description changed: %q", tool.Description)
	}
	wantSchema := object(map[string]any{
		"name": strProp("repository name"),
	}, "name")
	if !reflect.DeepEqual(tool.InputSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", tool.InputSchema, wantSchema)
	}

	result, err := tool.Handler(json.RawMessage(`{"name":"mcp-devbox"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "info-result" || service.name != "mcp-devbox" {
		t.Fatalf("result=%q name=%q", result, service.name)
	}
}
