package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

var safeGitHubRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

func safeGitHubRef(value string) bool {
	value = strings.TrimSpace(value)
	return safeGitHubRefPattern.MatchString(value) && !strings.Contains(value, "..") && !strings.Contains(value, "//") && !strings.Contains(value, "@{") && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, ".")
}

type githubRefResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}
type githubPullResponse struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	Mergeable *bool  `json:"mergeable"`
	Merged    bool   `json:"merged"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
}
type githubCheckRunsResponse struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"check_runs"`
}
type githubCombinedStatusResponse struct {
	State    string `json:"state"`
	Statuses []struct {
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
	} `json:"statuses"`
}
type githubMergeResponse struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}
type githubCheckSummary struct {
	Total, Pending, Failed, Passed int
	Lines                          []string
}

func (c *GitHubClient) branchSHA(ctx context.Context, repo, branch string) (string, error) {
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/git/ref/heads/" + url.PathEscape(branch)
	status, body, err := c.doJSON(ctx, http.MethodGet, path, nil)
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

func (c *GitHubClient) findPullRequest(ctx context.Context, repo, head, base string) (*githubPullResponse, error) {
	query := url.Values{}
	query.Set("state", "open")
	query.Set("head", c.owner+":"+head)
	query.Set("base", base)
	query.Set("per_page", "10")
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/pulls?" + query.Encode()
	status, body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub pull request lookup -> HTTP %d", status)
	}
	var pulls []githubPullResponse
	if err := json.Unmarshal([]byte(body), &pulls); err != nil {
		return nil, fmt.Errorf("decoding GitHub pull request lookup: %w", err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	return &pulls[0], nil
}

func (c *GitHubClient) pullRequest(ctx context.Context, repo string, number int) (githubPullResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(c.owner), url.PathEscape(repo), number)
	status, body, err := c.doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return githubPullResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPullResponse{}, fmt.Errorf("GitHub pull request -> HTTP %d", status)
	}
	var pull githubPullResponse
	if err := json.Unmarshal([]byte(body), &pull); err != nil {
		return githubPullResponse{}, fmt.Errorf("decoding GitHub pull request: %w", err)
	}
	return pull, nil
}

func (c *GitHubClient) createPullRequest(ctx context.Context, repo, head, base, title, description string) (githubPullResponse, error) {
	body, err := json.Marshal(map[string]any{"title": title, "head": head, "base": base, "body": description, "draft": false})
	if err != nil {
		return githubPullResponse{}, err
	}
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/pulls"
	status, responseBody, err := c.doJSON(ctx, http.MethodPost, path, body)
	if err != nil {
		return githubPullResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubPullResponse{}, fmt.Errorf("GitHub create pull request -> HTTP %d", status)
	}
	var pull githubPullResponse
	if err := json.Unmarshal([]byte(responseBody), &pull); err != nil {
		return githubPullResponse{}, fmt.Errorf("decoding created GitHub pull request: %w", err)
	}
	return pull, nil
}

func terminalSuccess(conclusion string) bool {
	return conclusion == "success" || conclusion == "neutral" || conclusion == "skipped"
}

func (c *GitHubClient) checkSummary(ctx context.Context, repo, sha string) (githubCheckSummary, error) {
	checksPath := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/commits/" + url.PathEscape(sha) + "/check-runs?per_page=100"
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, checksPath, nil, githubCheckRunsResponseLimit)
	if err != nil {
		return githubCheckSummary{}, err
	}
	if status < 200 || status >= 300 {
		return githubCheckSummary{}, fmt.Errorf("GitHub check runs -> HTTP %d", status)
	}
	var checks githubCheckRunsResponse
	if err := json.Unmarshal([]byte(body), &checks); err != nil {
		return githubCheckSummary{}, fmt.Errorf("decoding GitHub check runs: %w", err)
	}
	if checks.TotalCount != len(checks.CheckRuns) {
		return githubCheckSummary{}, fmt.Errorf("GitHub check run response was incomplete: expected %d, received %d", checks.TotalCount, len(checks.CheckRuns))
	}
	statusPath := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/commits/" + url.PathEscape(sha) + "/status"
	statusCode, statusBody, err := c.doJSON(ctx, http.MethodGet, statusPath, nil)
	if err != nil {
		return githubCheckSummary{}, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return githubCheckSummary{}, fmt.Errorf("GitHub commit status -> HTTP %d", statusCode)
	}
	var combined githubCombinedStatusResponse
	if err := json.Unmarshal([]byte(statusBody), &combined); err != nil {
		return githubCheckSummary{}, fmt.Errorf("decoding GitHub commit status: %w", err)
	}
	summary := githubCheckSummary{}
	for _, check := range checks.CheckRuns {
		summary.Total++
		line := fmt.Sprintf("check: %s | status=%s | conclusion=%s", check.Name, check.Status, check.Conclusion)
		if check.HTMLURL != "" {
			line += " | url=" + check.HTMLURL
		}
		summary.Lines = append(summary.Lines, line)
		if check.Status != "completed" {
			summary.Pending++
		} else if terminalSuccess(check.Conclusion) {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}
	for _, item := range combined.Statuses {
		summary.Total++
		line := fmt.Sprintf("status: %s | state=%s", item.Context, item.State)
		if item.TargetURL != "" {
			line += " | url=" + item.TargetURL
		}
		summary.Lines = append(summary.Lines, line)
		if item.State == "success" {
			summary.Passed++
		} else if item.State == "pending" {
			summary.Pending++
		} else {
			summary.Failed++
		}
	}
	sort.Strings(summary.Lines)
	return summary, nil
}

func (c *GitHubClient) mergePullRequest(ctx context.Context, repo string, number int, sha, title string) (githubMergeResponse, error) {
	body, err := json.Marshal(map[string]any{"sha": sha, "merge_method": "merge", "commit_title": title})
	if err != nil {
		return githubMergeResponse{}, err
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", url.PathEscape(c.owner), url.PathEscape(repo), number)
	status, responseBody, err := c.doJSON(ctx, http.MethodPut, path, body)
	if err != nil {
		return githubMergeResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubMergeResponse{}, fmt.Errorf("GitHub merge pull request -> HTTP %d", status)
	}
	var response githubMergeResponse
	if err := json.Unmarshal([]byte(responseBody), &response); err != nil {
		return githubMergeResponse{}, fmt.Errorf("decoding GitHub merge response: %w", err)
	}
	if !response.Merged || response.SHA == "" {
		return response, fmt.Errorf("GitHub did not merge pull request: %s", response.Message)
	}
	return response, nil
}

func (c *GitHubClient) updateDefaultBranch(ctx context.Context, repo, branch string) error {
	body, err := json.Marshal(map[string]any{"default_branch": branch})
	if err != nil {
		return err
	}
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo)
	status, _, err := c.doJSON(ctx, http.MethodPatch, path, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GitHub update default branch -> HTTP %d", status)
	}
	return nil
}

func validatePullInput(repo, head, base string) error {
	if !safeCloneDir(strings.TrimSpace(repo)) {
		return fmt.Errorf("invalid GitHub repository name %q", repo)
	}
	if !safeGitHubRef(head) || !safeGitHubRef(base) || head == base {
		return fmt.Errorf("invalid or identical GitHub head/base refs")
	}
	return nil
}

func (s *SourceCapability) SourcePullRequestCreatePreview(repo, head, base, title, description string) (string, error) {
	sp := s.log.Start("source_pull_request_create_preview")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	if err := validatePullInput(repo, head, base); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 256 || len(description) > 8192 {
		return "", fmt.Errorf("pull request title/body exceeds bounds or title is empty")
	}
	ctx := context.Background()
	headSHA, err := s.github.branchSHA(ctx, repo, head)
	if err != nil {
		return "", err
	}
	baseSHA, err := s.github.branchSHA(ctx, repo, base)
	if err != nil {
		return "", err
	}
	existing, err := s.github.findPullRequest(ctx, repo, head, base)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return fmt.Sprintf("pull_request: %d\nurl: %s\nstate: open\nexisting: true\n", existing.Number, existing.HTMLURL), fmt.Errorf("an open pull request already exists")
	}
	plan, err := s.plans.Create("source-pr-create", map[string]string{"repo": repo, "head": head, "base": base, "head_sha": headSHA, "base_sha": baseSHA, "title": s.redact(title), "description": s.redact(strings.TrimSpace(description))})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+repo+" "+plan.ID, nil, nil)
	return fmt.Sprintf("repository: %s/%s\nhead: %s\nhead_sha: %s\nbase: %s\nbase_sha: %s\neffect: create one non-draft pull request\nplan_id: %s\nexpiry: %s\n", s.github.owner, repo, head, headSHA, base, baseSHA, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourcePullRequestCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_pull_request_create")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_pull_request_create would execute the reviewed plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-pr-create")
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	headSHA, err := s.github.branchSHA(ctx, plan.Args["repo"], plan.Args["head"])
	if err != nil || headSHA != plan.Args["head_sha"] {
		return "", fmt.Errorf("pull request head changed after preview")
	}
	baseSHA, err := s.github.branchSHA(ctx, plan.Args["repo"], plan.Args["base"])
	if err != nil || baseSHA != plan.Args["base_sha"] {
		return "", fmt.Errorf("pull request base changed after preview")
	}
	existing, err := s.github.findPullRequest(ctx, plan.Args["repo"], plan.Args["head"], plan.Args["base"])
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("an open pull request appeared after preview")
	}
	pull, err := s.github.createPullRequest(ctx, plan.Args["repo"], plan.Args["head"], plan.Args["base"], plan.Args["title"], plan.Args["description"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("pull_request: %d\nurl: %s\nstate: %s\nhead_sha: %s\nbase: %s\n", pull.Number, pull.HTMLURL, pull.State, pull.Head.SHA, pull.Base.Ref), nil
}

func (s *SourceCapability) SourcePullRequestStatus(repo string, number int) (string, error) {
	sp := s.log.Start("source_pull_request_status")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	if !safeCloneDir(strings.TrimSpace(repo)) || number < 1 {
		return "", fmt.Errorf("invalid repository or pull request number")
	}
	pull, err := s.github.pullRequest(context.Background(), repo, number)
	if err != nil {
		return "", err
	}
	summary, err := s.github.checkSummary(context.Background(), repo, pull.Head.SHA)
	if err != nil {
		return "", err
	}
	allGreen := summary.Total > 0 && summary.Pending == 0 && summary.Failed == 0
	var b strings.Builder
	fmt.Fprintf(&b, "pull_request: %d\nurl: %s\nstate: %s\nmerged: %t\nhead: %s\nhead_sha: %s\nbase: %s\nmergeable: %s\nchecks_total: %d\nchecks_passed: %d\nchecks_pending: %d\nchecks_failed: %d\nall_checks_green: %t\n", pull.Number, pull.HTMLURL, pull.State, pull.Merged, pull.Head.Ref, pull.Head.SHA, pull.Base.Ref, nullableBool(pull.Mergeable), summary.Total, summary.Passed, summary.Pending, summary.Failed, allGreen)
	for _, line := range summary.Lines {
		b.WriteString(line + "\n")
	}
	sp.Finish(audit.Allow, repo+" #"+strconv.Itoa(number), nil, nil)
	return s.redact(b.String()), nil
}
func nullableBool(value *bool) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatBool(*value)
}

func (s *SourceCapability) SourcePullRequestMergePreview(repo string, number int) (string, error) {
	sp := s.log.Start("source_pull_request_merge_preview")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	if !safeCloneDir(strings.TrimSpace(repo)) || number < 1 {
		return "", fmt.Errorf("invalid repository or pull request number")
	}
	pull, err := s.github.pullRequest(context.Background(), repo, number)
	if err != nil {
		return "", err
	}
	if pull.State != "open" || pull.Merged || pull.Mergeable == nil || !*pull.Mergeable {
		return "", fmt.Errorf("pull request is not currently open and mergeable")
	}
	summary, err := s.github.checkSummary(context.Background(), repo, pull.Head.SHA)
	if err != nil {
		return "", err
	}
	if summary.Total == 0 || summary.Pending != 0 || summary.Failed != 0 {
		return "", fmt.Errorf("pull request checks are not completely green")
	}
	plan, err := s.plans.Create("source-pr-merge", map[string]string{"repo": repo, "number": strconv.Itoa(number), "head_sha": pull.Head.SHA, "base": pull.Base.Ref})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+repo+" #"+strconv.Itoa(number), nil, nil)
	return fmt.Sprintf("repository: %s/%s\npull_request: %d\nhead_sha: %s\nbase: %s\nchecks_total: %d\neffect: merge using a merge commit only\nplan_id: %s\nexpiry: %s\n", s.github.owner, repo, number, pull.Head.SHA, pull.Base.Ref, summary.Total, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourcePullRequestMerge(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_pull_request_merge")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_pull_request_merge would execute the reviewed green merge plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-pr-merge")
	if err != nil {
		return "", err
	}
	number, _ := strconv.Atoi(plan.Args["number"])
	pull, err := s.github.pullRequest(context.Background(), plan.Args["repo"], number)
	if err != nil {
		return "", err
	}
	if pull.State != "open" || pull.Merged || pull.Head.SHA != plan.Args["head_sha"] || pull.Mergeable == nil || !*pull.Mergeable {
		return "", fmt.Errorf("pull request state changed after merge preview")
	}
	summary, err := s.github.checkSummary(context.Background(), plan.Args["repo"], pull.Head.SHA)
	if err != nil || summary.Total == 0 || summary.Pending != 0 || summary.Failed != 0 {
		return "", fmt.Errorf("pull request checks changed after merge preview")
	}
	result, err := s.github.mergePullRequest(context.Background(), plan.Args["repo"], number, pull.Head.SHA, fmt.Sprintf("Merge pull request #%d from %s", number, pull.Head.Ref))
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("pull_request: %d\nmerged: true\nmerge_commit: %s\nmessage: %s\n", number, result.SHA, s.redact(result.Message)), nil
}

func (s *SourceCapability) SourceDefaultBranchUpdatePreview(repo, branch string) (string, error) {
	sp := s.log.Start("source_default_branch_update_preview")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	if !safeCloneDir(strings.TrimSpace(repo)) || !safeGitHubRef(branch) {
		return "", fmt.Errorf("invalid repository or branch")
	}
	status, body, err := s.github.repoInfo(context.Background(), repo)
	if err != nil || status < 200 || status >= 300 {
		return "", fmt.Errorf("reading repository before default branch update")
	}
	var current githubRepoResponse
	if err := json.Unmarshal([]byte(body), &current); err != nil {
		return "", err
	}
	sha, err := s.github.branchSHA(context.Background(), repo, branch)
	if err != nil {
		return "", err
	}
	plan, err := s.plans.Create("source-default-branch", map[string]string{"repo": repo, "branch": branch, "sha": sha, "previous": current.DefaultBranch})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+repo+" "+branch, nil, nil)
	return fmt.Sprintf("repository: %s/%s\ncurrent_default_branch: %s\nproposed_default_branch: %s\nbranch_sha: %s\neffect: update repository default branch\nplan_id: %s\nexpiry: %s\n", s.github.owner, repo, current.DefaultBranch, branch, sha, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourceDefaultBranchUpdate(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_default_branch_update")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_default_branch_update would execute the reviewed plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-default-branch")
	if err != nil {
		return "", err
	}
	status, body, err := s.github.repoInfo(context.Background(), plan.Args["repo"])
	if err != nil || status < 200 || status >= 300 {
		return "", fmt.Errorf("reading repository before default branch update execution")
	}
	var current githubRepoResponse
	if err := json.Unmarshal([]byte(body), &current); err != nil || current.DefaultBranch != plan.Args["previous"] {
		return "", fmt.Errorf("default branch state changed after preview")
	}
	sha, err := s.github.branchSHA(context.Background(), plan.Args["repo"], plan.Args["branch"])
	if err != nil || sha != plan.Args["sha"] {
		return "", fmt.Errorf("default branch target changed after preview")
	}
	if err := s.github.updateDefaultBranch(context.Background(), plan.Args["repo"], plan.Args["branch"]); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("repository: %s/%s\ndefault_branch: %s\nbranch_sha: %s\n", s.github.owner, plan.Args["repo"], plan.Args["branch"], sha), nil
}
