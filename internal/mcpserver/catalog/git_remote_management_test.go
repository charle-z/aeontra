package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitRemoteManagementService struct {
	calls []string
}

func (f *fakeGitRemoteManagementService) RepoRemotePreview(repo, remote, repository string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo+":"+remote+":"+repository)
	return "preview-result", nil
}

func (f *fakeGitRemoteManagementService) RepoRemoteSet(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "set:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "set-result", nil
}

func TestRegisterGitRemoteManagementDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeGitRemoteManagementService{}
	var registered []Tool
	RegisterGitRemoteManagement(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"repo_remote_preview", "repo_remote_set"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"repo_remote_preview": "Create a read-only, exact, expiring and single-use plan to add or update one named Git remote in a jailed repository. The destination must be credential-free and stay under configured GITHUB_OWNER.",
		"repo_remote_set":     "Execute one reviewed repo_remote_preview plan. It revalidates the current remote state and runs exactly git remote add or git remote set-url; requires approval in ask mode.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"repo_remote_preview": object(map[string]any{
			"repo":       strProp("repository directory, absolute or relative to the workspace root"),
			"remote":     strProp("remote name, defaults to origin"),
			"repository": strProp("repository name under configured owner, or an allowed credential-free HTTPS/SSH GitHub URL"),
		}, "repo", "repository"),
		"repo_remote_set": object(map[string]any{
			"plan_id": strProp("plan id returned by repo_remote_preview"),
			"approve": boolProp("execute the remote plan when approval is required"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	preview, err := byName["repo_remote_preview"].Handler(json.RawMessage(`{"repo":"project","remote":"origin","repository":"mcp-devbox"}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	set, err := byName["repo_remote_set"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || set != "set-result" {
		t.Fatalf("set = %q, err=%v", set, err)
	}
	wantCalls := []string{"preview:project:origin:mcp-devbox", "set:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
