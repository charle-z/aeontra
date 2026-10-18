package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitReadService struct {
	calls []string
}

func (f *fakeGitReadService) GitStatus(repo ...string) (string, error) {
	selected := ""
	if len(repo) > 0 {
		selected = repo[0]
	}
	f.calls = append(f.calls, "status:"+selected)
	return "status-result", nil
}

func (f *fakeGitReadService) GitDiffIn(repo string, args ...string) (string, error) {
	f.calls = append(f.calls, "diff:"+repo)
	f.calls = append(f.calls, args...)
	return "diff-result", nil
}

func TestRegisterGitReadsDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeGitReadService{}
	var registered []Tool
	RegisterGitReads(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"git_status", "git_diff"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"git_status": "Show git working-tree status (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos.",
		"git_diff":   "Show a git diff (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos. Optional extra args (e.g. --staged or a pathspec).",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"git_status": object(map[string]any{
			"repo": strProp("optional repo directory, absolute or relative to the workspace root"),
		}),
		"git_diff": object(map[string]any{
			"repo": strProp("optional repo directory, absolute or relative to the workspace root"),
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "extra git diff arguments",
			},
		}),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	status, err := byName["git_status"].Handler(json.RawMessage(`{"repo":"project"}`))
	if err != nil || status != "status-result" {
		t.Fatalf("status = %q, err=%v", status, err)
	}
	diff, err := byName["git_diff"].Handler(json.RawMessage(`{"repo":"project","args":["--staged","README.md"]}`))
	if err != nil || diff != "diff-result" {
		t.Fatalf("diff = %q, err=%v", diff, err)
	}
	wantCalls := []string{"status:project", "diff:project", "--staged", "README.md"}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
