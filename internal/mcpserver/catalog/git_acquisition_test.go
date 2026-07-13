package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitAcquisitionService struct {
	calls []string
}

func (f *fakeGitAcquisitionService) GitClone(url, dir string, approve bool) (string, error) {
	f.calls = append(f.calls, "clone:"+url+":"+dir)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "clone-result", nil
}

func (f *fakeGitAcquisitionService) RepoFetch(repo, remote string, approve bool) (string, error) {
	f.calls = append(f.calls, "fetch:"+repo+":"+remote)
	if approve {
		f.calls = append(f.calls, "approved")
	}
	return "fetch-result", nil
}

func TestRegisterGitAcquisitionDefinesStableContractsAndRoutesHandlers(t *testing.T) {
	service := &fakeGitAcquisitionService{}
	var registered []Tool
	RegisterGitAcquisition(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	gotNames := make([]string, 0, len(registered))
	for _, tool := range registered {
		gotNames = append(gotNames, tool.Name)
		if tool.Version != "1" {
			t.Fatalf("tool %s version = %q", tool.Name, tool.Version)
		}
	}
	wantNames := []string{"git_clone", "repo_fetch"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}

	wantDescriptions := map[string]string{
		"git_clone":  "Clone a Git repository into a new simple directory under the workspace root. No embedded credentials in URLs; target cannot escape the jail. Denied in read-only; in ask mode set approve=true.",
		"repo_fetch": "Fetch one named remote into one jailed Git repository by running exactly 'git fetch <remote>'. No refspecs or extra arguments are accepted. This external action updates local remote-tracking refs and requires approval in ask mode.",
	}
	byName := map[string]Tool{}
	for _, tool := range registered {
		byName[tool.Name] = tool
		if tool.Description != wantDescriptions[tool.Name] {
			t.Fatalf("%s description changed", tool.Name)
		}
	}

	wantSchemas := map[string]map[string]any{
		"git_clone": object(map[string]any{
			"url":     strProp("remote Git URL, without embedded credentials"),
			"dir":     strProp("optional simple target directory name under the workspace root; inferred from URL when omitted"),
			"approve": boolProp("clone even when approval is required"),
		}, "url"),
		"repo_fetch": object(map[string]any{
			"repo":    strProp("repository directory, absolute or relative to the workspace root"),
			"remote":  strProp("remote name, defaults to origin; option-like names are rejected"),
			"approve": boolProp("execute the fetch when approval is required"),
		}, "repo"),
	}
	for name, want := range wantSchemas {
		if !reflect.DeepEqual(byName[name].InputSchema, want) {
			t.Fatalf("%s schema = %#v, want %#v", name, byName[name].InputSchema, want)
		}
	}

	clone, err := byName["git_clone"].Handler(json.RawMessage(`{"url":"https://github.com/charle-z/app.git","dir":"app","approve":true}`))
	if err != nil || clone != "clone-result" {
		t.Fatalf("clone = %q, err=%v", clone, err)
	}
	fetch, err := byName["repo_fetch"].Handler(json.RawMessage(`{"repo":"app","remote":"origin","approve":true}`))
	if err != nil || fetch != "fetch-result" {
		t.Fatalf("fetch = %q, err=%v", fetch, err)
	}
	wantCalls := []string{
		"clone:https://github.com/charle-z/app.git:app", "approved",
		"fetch:app:origin", "approved",
	}
	if !reflect.DeepEqual(service.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", service.calls, wantCalls)
	}
}
