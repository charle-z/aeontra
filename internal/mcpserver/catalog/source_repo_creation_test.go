package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeSourceRepoCreationService struct {
	calls []string
}

func (f *fakeSourceRepoCreationService) SourceRepoCreate(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "create:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "create-result", nil
}

func (f *fakeSourceRepoCreationService) SourceRepoCreatePreview(name, visibility, description string) (string, error) {
	f.calls = append(f.calls, "preview:"+name+":"+visibility+":"+description)
	return "preview-result", nil
}

func TestRegisterSourceRepoCreationDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeSourceRepoCreationService{}
	var registered []Tool
	RegisterSourceRepoCreation(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"github_create_repo", "source_repo_create_preview"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"github_create_repo":         "Execute a previously reviewed source_repo_create_preview plan to create one GitHub repository under the configured owner. The plan is exact, expiring and single-use; token is never exposed; requires approval in ask mode.",
		"source_repo_create_preview": "Check that a repository is absent under the configured GitHub owner and create a read-only, exact, expiring and single-use creation plan. Private is the default; public must be explicit. Nothing is created.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"github_create_repo": object(map[string]any{
			"plan_id": strProp("plan id returned by source_repo_create_preview"),
			"approve": boolProp("execute the create plan when approval is required"),
		}, "plan_id"),
		"source_repo_create_preview": object(map[string]any{
			"name":        strProp("new repository name under the configured owner"),
			"visibility":  strProp("optional private or public visibility; defaults to configured private posture"),
			"description": strProp("optional repository description; redacted before planning"),
		}, "name"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	create, err := byName["github_create_repo"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || create != "create-result" {
		t.Fatalf("create = %q, err=%v", create, err)
	}
	preview, err := byName["source_repo_create_preview"].Handler(json.RawMessage(`{"name":"new-repo","visibility":"private","description":"desc"}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	wantCalls := []string{"create:plan-1", "approved", "preview:new-repo:private:desc"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
