//go:build !windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type projectGitHubRunnerFixture struct{ calls [][]string }

func (runner *projectGitHubRunnerFixture) Run(_ context.Context, arguments []string, _ edgeclient.GitHubCredential) (string, error) {
	runner.calls = append(runner.calls, append([]string(nil), arguments...))
	endpoint := arguments[3]
	switch {
	case strings.HasSuffix(endpoint, "/pulls"):
		return `[]`, nil
	case strings.HasSuffix(endpoint, "/actions/runs"):
		return `{"total_count":0,"workflow_runs":[]}`, nil
	case endpoint == "repos/charle-z/project":
		return `{"name":"project","full_name":"charle-z/project","visibility":"private","default_branch":"main","archived":false,"permissions":{"pull":true,"push":true,"admin":false}}`, nil
	default:
		return "", errors.New("unexpected endpoint")
	}
}

func TestCollectProjectGitHubStatusBindsResolvedOwnerAndSafeCapabilities(t *testing.T) {
	runner := &projectGitHubRunnerFixture{}
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "project"},
		Workspace:   edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
		TargetAlias: "parrot",
	}
	credential := edgeclient.GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"}
	result, err := collectProjectGitHubStatus(context.Background(), resolved, credential, runner)
	if err != nil || !result.GitHubConfigured || !result.GitHubActionsRead || !result.GitHubPullRequestsRead || result.ProjectRepository != "project" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	resolved.Project.Owner = "other"
	if _, err := collectProjectGitHubStatus(context.Background(), resolved, credential, runner); err == nil {
		t.Fatal("owner confusion accepted")
	}
}
