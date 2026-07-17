package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	githubDefaultResponseLimit      int64 = 16 << 10
	githubRefAndMergeResponseLimit  int64 = 64 << 10
	githubRepoMetadataResponseLimit int64 = 256 << 10
	githubPullResponseLimit         int64 = 512 << 10
	githubPullListResponseLimit     int64 = 1 << 20
	githubCheckRunsResponseLimit    int64 = 1 << 20
)

// GitHubClient is a narrow, token-backed GitHub API client for global-builder
// repo creation and lookup. The token is sent only as an Authorization header.
type GitHubClient struct {
	baseURL           string
	token             string
	owner             string
	ownerType         string // user|org
	defaultVisibility string // private|public
	do                func(*http.Request) (*http.Response, error)
}

func NewGitHubClient(baseURL, token, owner, ownerType, defaultVisibility string) *GitHubClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.github.com"
	}
	ownerType = strings.ToLower(strings.TrimSpace(ownerType))
	if ownerType == "" {
		ownerType = "user"
	}
	defaultVisibility = normalizeRepoVisibility(defaultVisibility)
	return &GitHubClient{
		baseURL:           strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:             strings.TrimSpace(token),
		owner:             strings.TrimSpace(owner),
		ownerType:         ownerType,
		defaultVisibility: defaultVisibility,
		do:                (&http.Client{Timeout: 30 * time.Second}).Do,
	}
}

func (c *GitHubClient) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != "" && c.owner != "" &&
		(c.ownerType == "user" || c.ownerType == "org")
}

type githubRepoResponse struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Visibility    string `json:"visibility"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	RoleName      string `json:"role_name"`
	Permissions   struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

func (c *GitHubClient) createRepo(ctx context.Context, name, description, visibility string) (int, string, error) {
	path := "/user/repos"
	if c.ownerType == "org" {
		path = "/orgs/" + url.PathEscape(c.owner) + "/repos"
	}
	private := normalizeRepoVisibility(visibility) != "public"
	body, err := json.Marshal(map[string]any{
		"name":        name,
		"description": description,
		"private":     private,
	})
	if err != nil {
		return 0, "", err
	}
	return c.doJSONLimit(ctx, http.MethodPost, path, body, githubRepoMetadataResponseLimit)
}

func (c *GitHubClient) repoInfo(ctx context.Context, name string) (int, string, error) {
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(name)
	return c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRepoMetadataResponseLimit)
}

func (c *GitHubClient) doJSON(ctx context.Context, method, path string, body []byte) (int, string, error) {
	return c.doJSONLimit(ctx, method, path, body, githubDefaultResponseLimit)
}

func (c *GitHubClient) doJSONLimit(ctx context.Context, method, path string, body []byte, limit int64) (int, string, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if limit < 1 {
		return resp.StatusCode, "", fmt.Errorf("invalid GitHub response limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("reading GitHub response: %w", err)
	}
	if int64(len(data)) > limit {
		return resp.StatusCode, "", fmt.Errorf("GitHub response exceeded %d-byte limit", limit)
	}
	return resp.StatusCode, strings.TrimSpace(string(data)), nil
}

func (s *SourceCapability) GitHubCreateRepo(name, description, visibility string, approve bool) (string, error) {
	sp := s.log.Start("github_create_repo")
	if s.github == nil || !s.github.Configured() {
		err := fmt.Errorf("github_create_repo is not configured (set GITHUB_TOKEN, GITHUB_OWNER, and GITHUB_OWNER_TYPE)")
		sp.Finish(audit.Deny, "github_create_repo", nil, err)
		return "", err
	}
	name = strings.TrimSpace(name)
	if !safeCloneDir(name) {
		err := fmt.Errorf("invalid GitHub repo name %q", name)
		sp.Finish(audit.Deny, "github_create_repo", nil, err)
		return "", err
	}
	visibility = normalizeRepoVisibilityWithDefault(visibility, s.github.defaultVisibility)
	if visibility == "" {
		err := fmt.Errorf("invalid visibility %q (use private or public)", visibility)
		sp.Finish(audit.Deny, "github_create_repo "+name, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, "github_create_repo "+name, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "github_create_repo "+name, nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: github_create_repo would create %s as %s. Re-invoke with approve=true.", name, visibility), nil
	}
	status, body, err := s.github.createRepo(context.Background(), name, description, visibility)
	if err != nil {
		sp.Finish(audit.Error, "github_create_repo "+name, nil, err)
		return "", fmt.Errorf("github create repo request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		sp.Finish(audit.Error, "github_create_repo "+name, nil, fmt.Errorf("HTTP %d", status))
		return s.redact(body), fmt.Errorf("github create repo -> HTTP %d: %s", status, s.redact(body))
	}
	sp.Finish(audit.Allow, "github_create_repo "+name, nil, nil)
	return s.formatGitHubRepo(body, "github repo created")
}

func (s *SourceCapability) GitHubRepoInfo(name string) (string, error) {
	sp := s.log.Start("github_repo_info")
	if s.github == nil || !s.github.Configured() {
		err := fmt.Errorf("github_repo_info is not configured (set GITHUB_TOKEN, GITHUB_OWNER, and GITHUB_OWNER_TYPE)")
		sp.Finish(audit.Deny, "github_repo_info", nil, err)
		return "", err
	}
	name = strings.TrimSpace(name)
	if !safeCloneDir(name) {
		err := fmt.Errorf("invalid GitHub repo name %q", name)
		sp.Finish(audit.Deny, "github_repo_info", nil, err)
		return "", err
	}
	status, body, err := s.github.repoInfo(context.Background(), name)
	if err != nil {
		sp.Finish(audit.Error, "github_repo_info "+name, nil, err)
		return "", fmt.Errorf("github repo info request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		sp.Finish(audit.Error, "github_repo_info "+name, nil, fmt.Errorf("HTTP %d", status))
		return s.redact(body), fmt.Errorf("github repo info -> HTTP %d: %s", status, s.redact(body))
	}
	sp.Finish(audit.Allow, "github_repo_info "+name, nil, nil)
	return s.formatGitHubRepo(body, "github repo")
}

func (s *SourceCapability) formatGitHubRepo(body, header string) (string, error) {
	var repo githubRepoResponse
	if err := json.Unmarshal([]byte(body), &repo); err != nil {
		return s.redact(body), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", header, repo.FullName)
	fmt.Fprintf(&b, "private: %t\n", repo.Private)
	if repo.DefaultBranch != "" {
		fmt.Fprintf(&b, "default_branch: %s\n", repo.DefaultBranch)
	}
	if repo.HTMLURL != "" {
		fmt.Fprintf(&b, "html_url: %s\n", repo.HTMLURL)
	}
	if repo.CloneURL != "" {
		fmt.Fprintf(&b, "clone_url: %s\n", repo.CloneURL)
	}
	if repo.SSHURL != "" {
		fmt.Fprintf(&b, "ssh_url: %s\n", repo.SSHURL)
	}
	return s.redact(b.String()), nil
}

func normalizeRepoVisibilityWithDefault(v, fallback string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return normalizeRepoVisibility(fallback)
	}
	if v == "private" || v == "public" {
		return v
	}
	return ""
}

func normalizeRepoVisibility(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "public":
		return "public"
	default:
		return "private"
	}
}
