package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type fakeGitHubCommandRunner struct {
	token     string
	responses map[string]string
	errors    map[string]error
	calls     [][]string
}

func (runner *fakeGitHubCommandRunner) Run(_ context.Context, arguments []string, credential GitHubCredential) (string, error) {
	if credential.Token != runner.token {
		return "", errors.New("wrong private credential")
	}
	for _, argument := range arguments {
		if strings.Contains(argument, runner.token) {
			return "", errors.New("token entered argv")
		}
	}
	runner.calls = append(runner.calls, append([]string(nil), arguments...))
	key := strings.Join(arguments, "\x00")
	return runner.responses[key], runner.errors[key]
}

func TestGitHubBrokerStatusUsesClosedGHCommandsAndReturnsBoundedCapabilities(t *testing.T) {
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	repositoryCall := []string{"api", "--method", "GET", "repos/charle-z/project"}
	pullsCall := []string{"api", "--method", "GET", "repos/charle-z/project/pulls", "-f", "state=all", "-F", "per_page=1"}
	actionsCall := []string{"api", "--method", "GET", "repos/charle-z/project/actions/runs", "-F", "per_page=1"}
	runner := &fakeGitHubCommandRunner{token: token, responses: map[string]string{
		strings.Join(repositoryCall, "\x00"): `{"name":"project","full_name":"charle-z/project","visibility":"private","default_branch":"main","archived":false,"permissions":{"pull":true,"push":true,"admin":false}}`,
		strings.Join(pullsCall, "\x00"):      `[]`,
		strings.Join(actionsCall, "\x00"):    `{"total_count":0,"workflow_runs":[]}`,
	}, errors: map[string]error{}}
	broker := NewGitHubBrokerClient(GitHubBrokerClientConfig{Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: token}, Runner: runner})
	status, err := broker.Status(context.Background(), "project")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Owner != "charle-z" || status.Repository != "project" || status.Visibility != "private" || status.DefaultBranch != "main" || status.Archived {
		t.Fatalf("status=%+v", status)
	}
	if !status.Capabilities.MetadataRead || !status.Capabilities.ContentsRead || !status.Capabilities.ContentsWrite || !status.Capabilities.PullRequestsRead || !status.Capabilities.ActionsRead || status.Capabilities.Administration {
		t.Fatalf("capabilities=%+v", status.Capabilities)
	}
	wantCalls := [][]string{repositoryCall, pullsCall, actionsCall}
	if !slices.EqualFunc(runner.calls, wantCalls, func(left, right []string) bool { return slices.Equal(left, right) }) {
		t.Fatalf("calls=%v", runner.calls)
	}
	body, _ := json.Marshal(status)
	if strings.Contains(string(body), token) {
		t.Fatalf("safe status leaked token: %s", body)
	}
}

func TestGitHubBrokerStatusDiagnosesClosedPermissionFailuresWithoutLeaking(t *testing.T) {
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	repositoryCall := []string{"api", "--method", "GET", "repos/charle-z/project"}
	runner := &fakeGitHubCommandRunner{token: token, responses: map[string]string{
		strings.Join(repositoryCall, "\x00"): `{"name":"project","full_name":"charle-z/project","visibility":"public","default_branch":"trunk","archived":true,"permissions":{"pull":true,"push":false,"admin":false}}`,
	}, errors: map[string]error{}}
	runner.errors[strings.Join([]string{"api", "--method", "GET", "repos/charle-z/project/pulls", "-f", "state=all", "-F", "per_page=1"}, "\x00")] = errors.New("secret response")
	runner.errors[strings.Join([]string{"api", "--method", "GET", "repos/charle-z/project/actions/runs", "-F", "per_page=1"}, "\x00")] = errors.New("secret response")
	broker := NewGitHubBrokerClient(GitHubBrokerClientConfig{Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: token}, Runner: runner})
	status, err := broker.Status(context.Background(), "project")
	if err != nil {
		t.Fatal(err)
	}
	if status.Capabilities.PullRequestsRead || status.Capabilities.ActionsRead || !slices.Equal(status.PermissionIssues, []string{"contents_write_denied", "pull_requests_read_denied", "actions_read_denied"}) {
		t.Fatalf("status=%+v", status)
	}
}

func TestGitHubBrokerStatusRejectsOwnerConfusionAndOversizedResponses(t *testing.T) {
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	runner := &fakeGitHubCommandRunner{token: token, responses: map[string]string{}, errors: map[string]error{}}
	runner.responses[strings.Join([]string{"api", "--method", "GET", "repos/charle-z/project"}, "\x00")] = strings.Repeat("x", 300<<10)
	broker := NewGitHubBrokerClient(GitHubBrokerClientConfig{Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: token}, Runner: runner})
	if _, err := broker.Status(context.Background(), "../other"); err == nil {
		t.Fatal("unsafe repository accepted")
	}
	if _, err := broker.Status(context.Background(), "project"); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("oversized response err=%v", err)
	}
}
