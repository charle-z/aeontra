package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
)

type GitHubBrokerClientConfig struct {
	Credential GitHubCredential
	Runner     GitHubCommandRunner
}

type GitHubCommandRunner interface {
	Run(context.Context, []string, GitHubCredential) (string, error)
}

type GitHubBrokerCapabilities struct {
	MetadataRead     bool `json:"metadata_read"`
	ContentsRead     bool `json:"contents_read"`
	ContentsWrite    bool `json:"contents_write"`
	PullRequestsRead bool `json:"pull_requests_read"`
	ActionsRead      bool `json:"actions_read"`
	Administration   bool `json:"administration"`
}

type GitHubBrokerStatus struct {
	Configured       bool                     `json:"configured"`
	Owner            string                   `json:"owner"`
	Repository       string                   `json:"repository"`
	Visibility       string                   `json:"visibility"`
	DefaultBranch    string                   `json:"default_branch"`
	Archived         bool                     `json:"archived"`
	Capabilities     GitHubBrokerCapabilities `json:"capabilities"`
	PermissionIssues []string                 `json:"permission_issues"`
}

type GitHubBrokerClient struct {
	credential GitHubCredential
	runner     GitHubCommandRunner
}

func NewGitHubBrokerClient(config GitHubBrokerClientConfig) *GitHubBrokerClient {
	return &GitHubBrokerClient{credential: config.Credential, runner: config.Runner}
}

func (client *GitHubBrokerClient) Status(ctx context.Context, repository string) (GitHubBrokerStatus, error) {
	if client == nil || client.runner == nil || !devGitSimplePattern.MatchString(repository) ||
		client.credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(client.credential.Owner) || !validGitHubToken(client.credential.Token) {
		return GitHubBrokerStatus{}, errors.New("GitHub broker status request is invalid")
	}
	endpoint := "repos/" + client.credential.Owner + "/" + repository
	body, err := client.runner.Run(ctx, []string{"api", "--method", "GET", endpoint}, client.credential)
	if err != nil {
		return GitHubBrokerStatus{}, errors.New("GitHub repository metadata is unavailable")
	}
	var metadata struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Visibility    string `json:"visibility"`
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Permissions   struct {
			Pull  bool `json:"pull"`
			Push  bool `json:"push"`
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if len(body) > 256<<10 || json.Unmarshal([]byte(body), &metadata) != nil || metadata.Name != repository ||
		metadata.FullName != client.credential.Owner+"/"+repository ||
		(metadata.Visibility != "private" && metadata.Visibility != "public" && metadata.Visibility != "internal") || !validDevGitBranch(metadata.DefaultBranch) {
		return GitHubBrokerStatus{}, errors.New("GitHub repository metadata is unavailable")
	}
	status := GitHubBrokerStatus{
		Configured: true, Owner: client.credential.Owner, Repository: repository,
		Visibility: metadata.Visibility, DefaultBranch: metadata.DefaultBranch, Archived: metadata.Archived,
		Capabilities: GitHubBrokerCapabilities{
			MetadataRead: true, ContentsRead: metadata.Permissions.Pull, ContentsWrite: metadata.Permissions.Push, Administration: metadata.Permissions.Admin,
		},
		PermissionIssues: make([]string, 0, 4),
	}
	if !status.Capabilities.ContentsRead {
		status.PermissionIssues = append(status.PermissionIssues, "contents_read_denied")
	}
	if !status.Capabilities.ContentsWrite {
		status.PermissionIssues = append(status.PermissionIssues, "contents_write_denied")
	}
	status.Capabilities.PullRequestsRead = client.permissionProbe(ctx, []string{"api", "--method", "GET", endpoint + "/pulls", "-f", "state=all", "-F", "per_page=1"})
	if !status.Capabilities.PullRequestsRead {
		status.PermissionIssues = append(status.PermissionIssues, "pull_requests_read_denied")
	}
	status.Capabilities.ActionsRead = client.permissionProbe(ctx, []string{"api", "--method", "GET", endpoint + "/actions/runs", "-F", "per_page=1"})
	if !status.Capabilities.ActionsRead {
		status.PermissionIssues = append(status.PermissionIssues, "actions_read_denied")
	}
	return status, nil
}

func (client *GitHubBrokerClient) permissionProbe(ctx context.Context, arguments []string) bool {
	_, err := client.runner.Run(ctx, arguments, client.credential)
	return err == nil
}
