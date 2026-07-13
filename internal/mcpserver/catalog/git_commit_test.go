package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fakeGitCommitService struct {
	repo    string
	message string
	approve bool
}

func (f *fakeGitCommitService) GitCommitIn(repo, message string, approve bool) (string, error) {
	f.repo = repo
	f.message = message
	f.approve = approve
	return "commit-result", nil
}

func TestRegisterGitCommitDefinesStableContractAndRoutesHandler(t *testing.T) {
	service := &fakeGitCommitService{}
	var registered []Tool
	RegisterGitCommit(func(tool Tool) {
		registered = append(registered, tool)
	}, service)

	if len(registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(registered))
	}
	tool := registered[0]
	if tool.Name != "git_commit" || tool.Version != "1" {
		t.Fatalf("tool = %s v%s", tool.Name, tool.Version)
	}
	wantDescription := "Stage all changes and commit them in the root or optional selected repo. Write action: denied in read-only; in ask mode set approve=true. Does not push."
	if tool.Description != wantDescription {
		t.Fatalf("description changed: %q", tool.Description)
	}
	wantSchema := object(map[string]any{
		"message": strProp("commit message"),
		"approve": boolProp("commit even when approval is required"),
		"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
	}, "message")
	if !reflect.DeepEqual(tool.InputSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", tool.InputSchema, wantSchema)
	}

	result, err := tool.Handler(json.RawMessage(`{"message":"Step 49","approve":true,"repo":"project"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "commit-result" {
		t.Fatalf("result = %q", result)
	}
	if service.repo != "project" || service.message != "Step 49" || !service.approve {
		t.Fatalf("routed repo=%q message=%q approve=%t", service.repo, service.message, service.approve)
	}
}
