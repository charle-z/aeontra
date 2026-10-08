package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitFastForwardService struct {
	calls []string
}

func (f *fakeGitFastForwardService) RepoFastForwardPreview(repo string) (string, error) {
	f.calls = append(f.calls, "preview:"+repo)
	return "preview-result", nil
}

func (f *fakeGitFastForwardService) RepoFastForward(planID string, approve bool) (string, error) {
	f.calls = append(f.calls, "execute:"+planID)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "execute-result", nil
}

func TestRegisterGitFastForwardDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeGitFastForwardService{}
	var registered []Tool
	RegisterGitFastForward(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"repo_fast_forward_preview", "repo_fast_forward"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"repo_fast_forward_preview": "Create a read-only, short-lived, single-use plan for an exact clean-tree fast-forward of the current attached branch to its existing upstream tracking ref. It does not fetch or modify the repository.",
		"repo_fast_forward":         "Execute one previously reviewed, unexpired and unused fast-forward plan using exactly 'git merge --ff-only <upstream>' inside the attested private L3 executor. Repository, branch, HEAD, target and clean state are revalidated; available only in administrator-selected allow mode with no host fallback.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"repo_fast_forward_preview": object(map[string]any{
			"repo": strProp("repository directory, absolute or relative to the workspace root"),
		}, "repo"),
		"repo_fast_forward": object(map[string]any{
			"plan_id": strProp("plan id returned by repo_fast_forward_preview"),
			"approve": boolProp("legacy compatibility field; does not grant execution authority"),
		}, "plan_id"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	preview, err := byName["repo_fast_forward_preview"].Handler(json.RawMessage(`{"repo":"project"}`))
	if err != nil || preview != "preview-result" {
		t.Fatalf("preview = %q, err=%v", preview, err)
	}
	execute, err := byName["repo_fast_forward"].Handler(json.RawMessage(`{"plan_id":"plan-1","approve":true}`))
	if err != nil || execute != "execute-result" {
		t.Fatalf("execute = %q, err=%v", execute, err)
	}
	wantCalls := []string{"preview:project", "execute:plan-1", "approved"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
