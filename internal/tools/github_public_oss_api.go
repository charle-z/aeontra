package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	githubPublicOSSResponseLimit int64 = 1 << 20
	githubPublicOSSMaxPages            = 10
)

var safeGitHubOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func safeGitHubOwner(value string) bool {
	value = strings.TrimSpace(value)
	return safeGitHubOwnerPattern.MatchString(value)
}

func splitFullRepo(value string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || !safeGitHubOwner(parts[0]) || !safeCloneDir(parts[1]) {
		return "", "", fmt.Errorf("invalid GitHub repository %q", value)
	}
	return parts[0], parts[1], nil
}

type githubPublicRepoResponse struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
	Fork          bool   `json:"fork"`
	Parent        *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
	Permissions struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

type githubPublicIssueResponse struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	Title     string `json:"title"`
	HTMLURL   string `json:"html_url"`
	UpdatedAt string `json:"updated_at"`
	Comments  int    `json:"comments"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

type githubPublicCommentResponse struct {
	ID        int64  `json:"id"`
	HTMLURL   string `json:"html_url"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

type githubPublicTimelineEvent struct {
	Event  string `json:"event"`
	Source *struct {
		Issue struct {
			HTMLURL     string          `json:"html_url"`
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
	} `json:"source"`
}

type githubPublicPullResponse struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	Mergeable *bool  `json:"mergeable"`
	Merged    bool   `json:"merged"`
	Head      struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
}

type githubPublicReviewResponse struct {
	ID          int64  `json:"id"`
	State       string `json:"state"`
	Body        string `json:"body"`
	SubmittedAt string `json:"submitted_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

type githubPublicReviewCommentResponse struct {
	ID             int64  `json:"id"`
	Body           string `json:"body"`
	Path           string `json:"path"`
	Line           int    `json:"line"`
	HTMLURL        string `json:"html_url"`
	PullRequestURL string `json:"pull_request_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	InReplyToID    int64  `json:"in_reply_to_id"`
	User           struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (c *GitHubClient) repoInfoAt(ctx context.Context, owner, repo string) (int, string, error) {
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	return c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRepoMetadataResponseLimit)
}

func (c *GitHubClient) publicRepo(ctx context.Context, owner, repo string) (githubPublicRepoResponse, error) {
	status, body, err := c.repoInfoAt(ctx, owner, repo)
	if err != nil {
		return githubPublicRepoResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicRepoResponse{}, fmt.Errorf("GitHub public repository lookup -> HTTP %d", status)
	}
	var result githubPublicRepoResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return githubPublicRepoResponse{}, fmt.Errorf("decoding GitHub public repository: %w", err)
	}
	if result.Private || strings.ToLower(strings.TrimSpace(result.Visibility)) == "private" {
		return githubPublicRepoResponse{}, fmt.Errorf("repository %s/%s is not public", owner, repo)
	}
	if result.FullName != owner+"/"+repo {
		return githubPublicRepoResponse{}, fmt.Errorf("GitHub repository identity mismatch")
	}
	return result, nil
}

func (c *GitHubClient) configuredFork(ctx context.Context, upstreamOwner, repo string) (githubPublicRepoResponse, bool, error) {
	status, body, err := c.repoInfoAt(ctx, c.owner, repo)
	if err != nil {
		return githubPublicRepoResponse{}, false, err
	}
	if status == http.StatusNotFound {
		return githubPublicRepoResponse{}, false, nil
	}
	if status < 200 || status >= 300 {
		return githubPublicRepoResponse{}, false, fmt.Errorf("GitHub fork lookup -> HTTP %d", status)
	}
	var result githubPublicRepoResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return githubPublicRepoResponse{}, false, fmt.Errorf("decoding configured GitHub fork: %w", err)
	}
	parent := ""
	if result.Parent != nil {
		parent = strings.TrimSpace(result.Parent.FullName)
	}
	if result.FullName != c.owner+"/"+repo || !result.Fork || parent != upstreamOwner+"/"+repo {
		return githubPublicRepoResponse{}, true, fmt.Errorf("configured repository %s/%s is not the expected fork of %s/%s", c.owner, repo, upstreamOwner, repo)
	}
	if result.Private || (!result.Permissions.Push && !result.Permissions.Admin) {
		return githubPublicRepoResponse{}, true, fmt.Errorf("configured fork is not public and writable")
	}
	return result, true, nil
}

func (c *GitHubClient) createFork(ctx context.Context, upstreamOwner, repo string) (githubPublicRepoResponse, error) {
	payload := map[string]any{}
	if c.ownerType == "org" {
		payload["organization"] = c.owner
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return githubPublicRepoResponse{}, err
	}
	path := "/repos/" + url.PathEscape(upstreamOwner) + "/" + url.PathEscape(repo) + "/forks"
	status, responseBody, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubRepoMetadataResponseLimit)
	if err != nil {
		return githubPublicRepoResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicRepoResponse{}, fmt.Errorf("GitHub create fork -> HTTP %d", status)
	}
	var result githubPublicRepoResponse
	if err := json.Unmarshal([]byte(responseBody), &result); err != nil {
		return githubPublicRepoResponse{}, fmt.Errorf("decoding created GitHub fork: %w", err)
	}
	return result, nil
}

func (c *GitHubClient) waitForConfiguredFork(ctx context.Context, upstreamOwner, repo string) (githubPublicRepoResponse, error) {
	for attempt := 0; attempt < 120; attempt++ {
		result, exists, err := c.configuredFork(ctx, upstreamOwner, repo)
		if err != nil {
			return githubPublicRepoResponse{}, err
		}
		if exists {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return githubPublicRepoResponse{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return githubPublicRepoResponse{}, fmt.Errorf("GitHub fork was not available after creation")
}

func (c *GitHubClient) publicIssue(ctx context.Context, owner, repo string, number int) (githubPublicIssueResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubPublicOSSResponseLimit)
	if err != nil {
		return githubPublicIssueResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicIssueResponse{}, fmt.Errorf("GitHub public issue lookup -> HTTP %d", status)
	}
	var result githubPublicIssueResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return githubPublicIssueResponse{}, fmt.Errorf("decoding GitHub public issue: %w", err)
	}
	return result, nil
}

func githubPublicPages[T any](ctx context.Context, client *GitHubClient, pathForPage func(int) string, label string) ([]T, error) {
	var result []T
	for page := 1; page <= githubPublicOSSMaxPages; page++ {
		status, body, err := client.doJSONLimit(ctx, http.MethodGet, pathForPage(page), nil, githubPublicOSSResponseLimit)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("GitHub %s -> HTTP %d", label, status)
		}
		var pageItems []T
		if err := json.Unmarshal([]byte(body), &pageItems); err != nil {
			return nil, fmt.Errorf("decoding GitHub %s: %w", label, err)
		}
		result = append(result, pageItems...)
		if len(pageItems) < 100 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("GitHub %s exceeded pagination safety limit", label)
}

func (c *GitHubClient) publicIssueComments(ctx context.Context, owner, repo string, number int) ([]githubPublicCommentResponse, error) {
	return githubPublicPages[githubPublicCommentResponse](ctx, c, func(page int) string {
		return fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
	}, "issue comments")
}

func (c *GitHubClient) publicIssueTimeline(ctx context.Context, owner, repo string, number int) ([]githubPublicTimelineEvent, error) {
	return githubPublicPages[githubPublicTimelineEvent](ctx, c, func(page int) string {
		return fmt.Sprintf("/repos/%s/%s/issues/%d/timeline?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
	}, "issue timeline")
}

func (c *GitHubClient) createPublicIssueComment(ctx context.Context, owner, repo string, number int, comment string) (githubPublicCommentResponse, error) {
	body, err := json.Marshal(map[string]string{"body": comment})
	if err != nil {
		return githubPublicCommentResponse{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(owner), url.PathEscape(repo), number)
	status, responseBody, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubPublicOSSResponseLimit)
	if err != nil {
		return githubPublicCommentResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicCommentResponse{}, fmt.Errorf("GitHub create issue comment -> HTTP %d", status)
	}
	var result githubPublicCommentResponse
	if err := json.Unmarshal([]byte(responseBody), &result); err != nil {
		return githubPublicCommentResponse{}, fmt.Errorf("decoding created GitHub issue comment: %w", err)
	}
	return result, nil
}

func (c *GitHubClient) branchSHAAt(ctx context.Context, owner, repo, branch string) (string, error) {
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/git/ref/heads/" + url.PathEscape(branch)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRefAndMergeResponseLimit)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("GitHub branch lookup -> HTTP %d", status)
	}
	var response githubRefResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil || response.Object.SHA == "" {
		return "", fmt.Errorf("decoding GitHub branch lookup")
	}
	return response.Object.SHA, nil
}

func (c *GitHubClient) compareAt(ctx context.Context, owner, repo, baseSHA, headSHA string) (string, error) {
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/compare/" + url.PathEscape(baseSHA) + "..." + url.PathEscape(headSHA)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubCompareResponseLimit)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("GitHub compare -> HTTP %d", status)
	}
	var response githubCompareResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return "", fmt.Errorf("decoding GitHub compare: %w", err)
	}
	return response.Status, nil
}

func (c *GitHubClient) findCrossRepoPullRequest(ctx context.Context, upstreamOwner, repo, head, base string) (*githubPublicPullResponse, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", c.owner+":"+head)
	query.Set("base", base)
	query.Set("per_page", "10")
	path := "/repos/" + url.PathEscape(upstreamOwner) + "/" + url.PathEscape(repo) + "/pulls?" + query.Encode()
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubPullListResponseLimit)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub cross-repository pull request lookup -> HTTP %d", status)
	}
	var pulls []githubPublicPullResponse
	if err := json.Unmarshal([]byte(body), &pulls); err != nil {
		return nil, fmt.Errorf("decoding cross-repository pull request lookup: %w", err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	return &pulls[0], nil
}

func (c *GitHubClient) createCrossRepoPullRequest(ctx context.Context, upstreamOwner, repo, head, base, title, description string, draft bool) (githubPublicPullResponse, error) {
	body, err := json.Marshal(map[string]any{"title": title, "head": c.owner + ":" + head, "base": base, "body": description, "draft": draft})
	if err != nil {
		return githubPublicPullResponse{}, err
	}
	path := "/repos/" + url.PathEscape(upstreamOwner) + "/" + url.PathEscape(repo) + "/pulls"
	status, responseBody, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubPullResponseLimit)
	if err != nil {
		return githubPublicPullResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicPullResponse{}, fmt.Errorf("GitHub create cross-repository pull request -> HTTP %d", status)
	}
	var result githubPublicPullResponse
	if err := json.Unmarshal([]byte(responseBody), &result); err != nil {
		return githubPublicPullResponse{}, fmt.Errorf("decoding created cross-repository pull request: %w", err)
	}
	return result, nil
}

func (c *GitHubClient) publicPullRequest(ctx context.Context, owner, repo string, number int) (githubPublicPullResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubPullResponseLimit)
	if err != nil {
		return githubPublicPullResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicPullResponse{}, fmt.Errorf("GitHub public pull request -> HTTP %d", status)
	}
	var result githubPublicPullResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return githubPublicPullResponse{}, fmt.Errorf("decoding GitHub public pull request: %w", err)
	}
	return result, nil
}

func (c *GitHubClient) publicReviewComments(ctx context.Context, owner, repo string, number int) ([]githubPublicReviewCommentResponse, error) {
	return githubPublicPages[githubPublicReviewCommentResponse](ctx, c, func(page int) string {
		return fmt.Sprintf("/repos/%s/%s/pulls/%d/comments?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
	}, "public review comments")
}

func (c *GitHubClient) publicReviewComment(ctx context.Context, owner, repo string, commentID int64) (githubPublicReviewCommentResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/comments/%d", url.PathEscape(owner), url.PathEscape(repo), commentID)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubPublicOSSResponseLimit)
	if err != nil {
		return githubPublicReviewCommentResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicReviewCommentResponse{}, fmt.Errorf("GitHub public review comment -> HTTP %d", status)
	}
	var result githubPublicReviewCommentResponse
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return githubPublicReviewCommentResponse{}, fmt.Errorf("decoding GitHub public review comment: %w", err)
	}
	if result.ID != commentID {
		return githubPublicReviewCommentResponse{}, fmt.Errorf("GitHub review comment identity mismatch")
	}
	return result, nil
}

func (c *GitHubClient) createPublicReviewReply(ctx context.Context, owner, repo string, number int, commentID int64, reply string) (githubPublicReviewCommentResponse, error) {
	body, err := json.Marshal(map[string]string{"body": reply})
	if err != nil {
		return githubPublicReviewCommentResponse{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments/%d/replies", url.PathEscape(owner), url.PathEscape(repo), number, commentID)
	status, responseBody, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubPublicOSSResponseLimit)
	if err != nil {
		return githubPublicReviewCommentResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPublicReviewCommentResponse{}, fmt.Errorf("GitHub create review reply -> HTTP %d", status)
	}
	var result githubPublicReviewCommentResponse
	if err := json.Unmarshal([]byte(responseBody), &result); err != nil {
		return githubPublicReviewCommentResponse{}, fmt.Errorf("decoding created GitHub review reply: %w", err)
	}
	return result, nil
}

func (c *GitHubClient) publicPullReviews(ctx context.Context, owner, repo string, number int) ([]githubPublicReviewResponse, error) {
	return githubPublicPages[githubPublicReviewResponse](ctx, c, func(page int) string {
		return fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
	}, "public pull request reviews")
}

func (c *GitHubClient) publicCheckRuns(ctx context.Context, owner, repo, sha string) (githubCheckRunsResponse, error) {
	var aggregate githubCheckRunsResponse
	expected := -1
	for page := 1; page <= githubEvidenceMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha), page)
		statusCode, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubCheckRunsResponseLimit)
		if err != nil {
			return githubCheckRunsResponse{}, err
		}
		if statusCode < 200 || statusCode >= 300 {
			return githubCheckRunsResponse{}, fmt.Errorf("GitHub public check runs -> HTTP %d", statusCode)
		}
		var response githubCheckRunsResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			return githubCheckRunsResponse{}, fmt.Errorf("decoding GitHub public check runs: %w", err)
		}
		if expected < 0 {
			expected = response.TotalCount
		} else if response.TotalCount != expected {
			return githubCheckRunsResponse{}, fmt.Errorf("GitHub public check run total changed during pagination")
		}
		aggregate.CheckRuns = append(aggregate.CheckRuns, response.CheckRuns...)
		if len(aggregate.CheckRuns) >= expected {
			if len(aggregate.CheckRuns) != expected {
				return githubCheckRunsResponse{}, fmt.Errorf("GitHub public check run response was inconsistent: expected %d, received %d", expected, len(aggregate.CheckRuns))
			}
			aggregate.TotalCount = expected
			return aggregate, nil
		}
		if len(response.CheckRuns) == 0 {
			return githubCheckRunsResponse{}, fmt.Errorf("GitHub public check run pagination was incomplete")
		}
	}
	return githubCheckRunsResponse{}, fmt.Errorf("GitHub public check run pagination exceeded safety limit")
}

func (c *GitHubClient) publicCheckSummary(ctx context.Context, fullRepo, sha string) (githubCheckSummary, error) {
	owner, repo, err := splitFullRepo(fullRepo)
	if err != nil {
		return githubCheckSummary{}, err
	}
	checks, err := c.publicCheckRuns(ctx, owner, repo, sha)
	if err != nil {
		return githubCheckSummary{}, err
	}
	summary := githubCheckSummary{Source: "public_checks_api", RunsTotal: len(checks.CheckRuns), EvidenceComplete: true}
	for _, check := range checks.CheckRuns {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			summary.EvidenceComplete = false
		}
		line := fmt.Sprintf("check: %s | status=%s | conclusion=%s", name, check.Status, check.Conclusion)
		if check.HTMLURL != "" {
			line += " | url=" + check.HTMLURL
		}
		summary.Lines = append(summary.Lines, line)
		addEvidenceCounts(&summary, check.Status, check.Conclusion)
	}
	statusPath := fmt.Sprintf("/repos/%s/%s/commits/%s/status", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	statusCode, body, err := c.doJSONLimit(ctx, http.MethodGet, statusPath, nil, githubCommitStatusResponseLimit)
	if err != nil {
		return githubCheckSummary{}, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return githubCheckSummary{}, fmt.Errorf("GitHub public commit status -> HTTP %d", statusCode)
	}
	var combined githubCombinedStatusResponse
	if err := json.Unmarshal([]byte(body), &combined); err != nil {
		return githubCheckSummary{}, fmt.Errorf("decoding GitHub public commit status: %w", err)
	}
	if combined.TotalCount != 0 && combined.TotalCount != len(combined.Statuses) {
		return githubCheckSummary{}, fmt.Errorf("GitHub public commit status response was incomplete")
	}
	summary.CommitStatuses = len(combined.Statuses)
	for _, item := range combined.Statuses {
		line := fmt.Sprintf("status: %s | state=%s", item.Context, item.State)
		if item.TargetURL != "" {
			line += " | url=" + item.TargetURL
		}
		summary.Lines = append(summary.Lines, line)
		switch item.State {
		case "success":
			summary.Passed++
		case "pending":
			summary.Pending++
		default:
			summary.Failed++
		}
	}
	evidence := summary.RunsTotal + summary.CommitStatuses
	summary.EvidenceComplete = summary.EvidenceComplete && evidence > 0
	summary.AllChecksGreen = summary.EvidenceComplete && summary.Pending == 0 && summary.Failed == 0
	sort.Strings(summary.Lines)
	return summary, nil
}
