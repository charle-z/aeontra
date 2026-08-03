package mcpserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestProjectGitHubStatusToolQueuesOnlyBoundProjectIdentity(t *testing.T) {
	store := &projectGitSyncToolStore{waitResult: edge.Operation{State: edge.OperationSucceeded, Result: edge.OperationResult{
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectTarget: "parrot",
		GitHubConfigured: true, GitHubVisibility: "private", GitHubDefaultBranch: "main",
		GitHubMetadataRead: true, GitHubContentsRead: true, GitHubContentsWrite: true, GitHubPullRequestsRead: true, GitHubActionsRead: true,
	}}}
	server := New(nil).WithEdgeStore(store)
	entry, ok := server.table["project_github_status"]
	if !ok {
		t.Fatal("project_github_status missing")
	}
	schema, _ := json.Marshal(entry.def.InputSchema)
	for _, forbidden := range []string{`"repository"`, `"owner"`, `"url"`, `"token"`, `"path"`} {
		if strings.Contains(string(schema), forbidden) {
			t.Fatalf("schema exposed %s: %s", forbidden, schema)
		}
	}
	output, err := server.handleProjectGitHubStatus(json.RawMessage(`{"alias":"project","target":"parrot"}`))
	if err != nil {
		t.Fatal(err)
	}
	wantRequest := edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}
	if store.createdKind != edge.OperationProjectGitHubStatus || !reflect.DeepEqual(store.createdRequest, wantRequest) {
		t.Fatalf("kind=%q request=%+v", store.createdKind, store.createdRequest)
	}
	for _, required := range []string{`"repository":"charle-z/repo"`, `"configured":true`, `"visibility":"private"`, `"default_branch":"main"`, `"metadata_read":true`, `"actions_read":true`} {
		if !strings.Contains(output, required) {
			t.Fatalf("output missing %s: %s", required, output)
		}
	}
	for _, forbidden := range []string{"workspace_id", "device_id", "token", "path", "credential"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output exposed %s: %s", forbidden, output)
		}
	}
}

func TestProjectGitHubStatusToolRejectsUnknownFields(t *testing.T) {
	store := &projectGitSyncToolStore{}
	server := New(nil).WithEdgeStore(store)
	if _, err := server.handleProjectGitHubStatus(json.RawMessage(`{"alias":"project","target":"parrot","repository":"other"}`)); err == nil {
		t.Fatal("caller repository accepted")
	}
	if store.createdKind != "" {
		t.Fatalf("operation queued: %s", store.createdKind)
	}
}
