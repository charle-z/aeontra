package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitPublicationService struct {
	calls []string
}

func (f *fakeGitPublicationService) RepoPublish(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "publish:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "publish-result", nil
}

func (f *fakeGitPublicationService) RepoPublishPreview(repo, remote, branch string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo+":"+remote+":"+branch)
	return "preview-result", nil
}

func TestRegisterGitPublicationDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeGitPublicationService{}
	var registered []Tool
	RegisterGitPublication(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"git_push", "repo_publish_preview"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"git_push":             "Execute a previously reviewed repo_publish_preview plan for one local branch and one named owner-restricted remote. No force, mirror, tags, refspecs, URL remotes, or extra arguments are accepted; requires approval in ask mode.",
		"repo_publish_preview": "Validate a clean attached current branch and one named credential-free GitHub remote, inspect the exact remote branch state, reject behind/diverged publication, and create a read-only expiring single-use push plan. It does not push.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"git_push": object(map[string]any{
			"plan_id": strProp("plan id returned by repo_publish_preview"),
			"approve": boolProp("execute the publication plan when approval is required"),
		}, "plan_id"),
		"repo_publish_preview": object(map[string]any{
			"repo":   strProp("repository directory, absolute or relative to the workspace root"),
			"remote": strProp("remote name, defaults to origin; URLs and option-like names are rejected"),
			"branch": strProp("branch name, defaults to and must equal the current attached branch"),
		}, "repo"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	publish, err := byName["git_push"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || publish != "publish-result" {
		t.Fatalf("publish = %q, err=%v", publish, err)
	}
	preview, err := byName["repo_publish_preview"].Handler(json.RawMessage(`{"repo":"project","remote":"origin","branch":"main"}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	wantCalls := []string{"publish:plan-1", "approved", "preview:project:origin:main"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
