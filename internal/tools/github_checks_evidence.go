package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const githubEvidenceMaxPages = 100

var errGitHubRequiredChecksUnavailable = errors.New("GitHub required status checks unavailable")

type githubActionsRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	RunAttempt int    `json:"run_attempt"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	HeadSHA    string `json:"head_sha"`
}

type githubActionsRunsResponse struct {
	TotalCount   int                `json:"total_count"`
	WorkflowRuns []githubActionsRun `json:"workflow_runs"`
}

type githubActionsJob struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

type githubActionsJobsResponse struct {
	TotalCount int                `json:"total_count"`
	Jobs       []githubActionsJob `json:"jobs"`
}

type githubRequiredStatusChecksResponse struct {
	Contexts []string `json:"contexts"`
	Checks   []struct {
		Context string `json:"context"`
	} `json:"checks"`
}

func pendingGitHubState(value string) bool {
	switch value {
	case "queued", "in_progress", "waiting", "requested", "pending":
		return true
	default:
		return false
	}
}

func classifyGitHubEvidence(status, conclusion string) (passed, pending, failed int) {
	if status != "completed" {
		if pendingGitHubState(status) {
			return 0, 1, 0
		}
		return 0, 0, 1
	}
	if terminalSuccess(conclusion) {
		return 1, 0, 0
	}
	return 0, 0, 1
}

func addEvidenceCounts(summary *githubCheckSummary, status, conclusion string) {
	passed, pending, failed := classifyGitHubEvidence(status, conclusion)
	summary.Passed += passed
	summary.Pending += pending
	summary.Failed += failed
}

func (c *GitHubClient) collectGitHubEvidence(ctx context.Context, repo, sha, base string) (githubCheckSummary, error) {
	checks, statusCode, err := c.githubCheckRuns(ctx, repo, sha)
	if err != nil {
		return githubCheckSummary{}, err
	}
	var summary githubCheckSummary
	if statusCode == http.StatusForbidden {
		summary, err = c.githubActionsEvidence(ctx, repo, sha, base)
		if err != nil {
			return githubCheckSummary{}, err
		}
	} else if statusCode < 200 || statusCode >= 300 {
		return githubCheckSummary{}, fmt.Errorf("GitHub check runs -> HTTP %d", statusCode)
	} else {
		summary, err = c.githubChecksAPIEvidence(ctx, repo, base, checks)
		if err != nil {
			return githubCheckSummary{}, err
		}
	}
	if err := c.addGitHubCommitStatuses(ctx, repo, sha, &summary); err != nil {
		return githubCheckSummary{}, err
	}
	evidence := summary.RunsTotal + summary.JobsTotal + summary.CommitStatuses
	if evidence == 0 {
		summary.EvidenceComplete = false
	}
	summary.AllChecksGreen = summary.EvidenceComplete && evidence > 0 && summary.Pending == 0 && summary.Failed == 0
	sort.Strings(summary.Lines)
	return summary, nil
}

func (c *GitHubClient) githubChecksAPIEvidence(ctx context.Context, repo, base string, checks githubCheckRunsResponse) (githubCheckSummary, error) {
	summary := githubCheckSummary{Source: "checks_api", RunsTotal: len(checks.CheckRuns), EvidenceComplete: true}
	present := make(map[string]struct{}, len(checks.CheckRuns))
	for _, check := range checks.CheckRuns {
		name := strings.TrimSpace(check.Name)
		if name == "" {
			summary.EvidenceComplete = false
		}
		present[name] = struct{}{}
		line := fmt.Sprintf("check: %s | status=%s | conclusion=%s", name, check.Status, check.Conclusion)
		if check.HTMLURL != "" {
			line += " | url=" + check.HTMLURL
		}
		summary.Lines = append(summary.Lines, line)
		addEvidenceCounts(&summary, check.Status, check.Conclusion)
	}
	if err := c.requireGitHubEvidence(ctx, repo, base, present, &summary); err != nil {
		return githubCheckSummary{}, err
	}
	return summary, nil
}

func (c *GitHubClient) githubCheckRuns(ctx context.Context, repo, sha string) (githubCheckRunsResponse, int, error) {
	var aggregate githubCheckRunsResponse
	expected := -1
	for page := 1; page <= githubEvidenceMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?per_page=100&page=%d", url.PathEscape(c.owner), url.PathEscape(repo), url.PathEscape(sha), page)
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubCheckRunsResponseLimit)
		if err != nil {
			return githubCheckRunsResponse{}, status, err
		}
		if status == http.StatusForbidden {
			return githubCheckRunsResponse{}, status, nil
		}
		if status < 200 || status >= 300 {
			return githubCheckRunsResponse{}, status, nil
		}
		var response githubCheckRunsResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			return githubCheckRunsResponse{}, status, fmt.Errorf("decoding GitHub check runs: %w", err)
		}
		if expected < 0 {
			expected = response.TotalCount
		} else if response.TotalCount != expected {
			return githubCheckRunsResponse{}, status, fmt.Errorf("GitHub check run total changed during pagination")
		}
		aggregate.CheckRuns = append(aggregate.CheckRuns, response.CheckRuns...)
		if len(aggregate.CheckRuns) >= expected {
			if len(aggregate.CheckRuns) != expected {
				return githubCheckRunsResponse{}, status, fmt.Errorf("GitHub check run response was incomplete: expected %d, received %d", expected, len(aggregate.CheckRuns))
			}
			aggregate.TotalCount = expected
			return aggregate, status, nil
		}
		if len(response.CheckRuns) == 0 {
			return githubCheckRunsResponse{}, status, fmt.Errorf("GitHub check run pagination was incomplete")
		}
	}
	return githubCheckRunsResponse{}, http.StatusOK, fmt.Errorf("GitHub check run pagination exceeded safety limit")
}

func (c *GitHubClient) githubActionsEvidence(ctx context.Context, repo, sha, base string) (githubCheckSummary, error) {
	runs, err := c.githubActionsRuns(ctx, repo, sha)
	if err != nil {
		return githubCheckSummary{}, err
	}
	latest := make(map[int64]githubActionsRun)
	for _, run := range runs {
		if run.WorkflowID <= 0 || run.ID <= 0 || run.RunAttempt < 1 || run.HeadSHA != sha || strings.TrimSpace(run.Name) == "" {
			return githubCheckSummary{}, fmt.Errorf("GitHub Actions run evidence was invalid")
		}
		current, ok := latest[run.WorkflowID]
		if !ok || run.RunAttempt > current.RunAttempt || (run.RunAttempt == current.RunAttempt && run.ID > current.ID) {
			latest[run.WorkflowID] = run
		}
	}
	summary := githubCheckSummary{Source: "actions_fallback", RunsTotal: len(latest), EvidenceComplete: len(latest) > 0}
	present := make(map[string]struct{})
	workflowIDs := make([]int64, 0, len(latest))
	for workflowID := range latest {
		workflowIDs = append(workflowIDs, workflowID)
	}
	sort.Slice(workflowIDs, func(i, j int) bool { return workflowIDs[i] < workflowIDs[j] })
	for _, workflowID := range workflowIDs {
		run := latest[workflowID]
		present[run.Name] = struct{}{}
		line := fmt.Sprintf("run: %s | workflow_id=%d | attempt=%d | status=%s | conclusion=%s", run.Name, run.WorkflowID, run.RunAttempt, run.Status, run.Conclusion)
		if run.HTMLURL != "" {
			line += " | url=" + run.HTMLURL
		}
		summary.Lines = append(summary.Lines, line)
		addEvidenceCounts(&summary, run.Status, run.Conclusion)

		jobs, err := c.githubActionsJobs(ctx, repo, run.ID)
		if err != nil {
			return githubCheckSummary{}, err
		}
		if len(jobs) == 0 {
			summary.EvidenceComplete = false
		}
		for _, job := range jobs {
			if job.ID <= 0 || strings.TrimSpace(job.Name) == "" {
				summary.EvidenceComplete = false
			}
			summary.JobsTotal++
			present[job.Name] = struct{}{}
			present[run.Name+" / "+job.Name] = struct{}{}
			jobLine := fmt.Sprintf("job: %s / %s | status=%s | conclusion=%s", run.Name, job.Name, job.Status, job.Conclusion)
			if job.HTMLURL != "" {
				jobLine += " | url=" + job.HTMLURL
			}
			summary.Lines = append(summary.Lines, jobLine)
			addEvidenceCounts(&summary, job.Status, job.Conclusion)
		}
	}
	if err := c.requireGitHubEvidence(ctx, repo, base, present, &summary); err != nil {
		return githubCheckSummary{}, err
	}
	return summary, nil
}

func (c *GitHubClient) githubActionsRuns(ctx context.Context, repo, sha string) ([]githubActionsRun, error) {
	all := make([]githubActionsRun, 0)
	expected := -1
	for page := 1; page <= githubEvidenceMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/actions/runs?head_sha=%s&per_page=100&page=%d", url.PathEscape(c.owner), url.PathEscape(repo), url.QueryEscape(sha), page)
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubActionsResponseLimit)
		if err != nil {
			return nil, err
		}
		if status == http.StatusForbidden {
			return nil, fmt.Errorf("GitHub Actions fallback -> HTTP 403")
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("GitHub Actions fallback -> HTTP %d", status)
		}
		var response githubActionsRunsResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			return nil, fmt.Errorf("decoding GitHub Actions runs: %w", err)
		}
		if expected < 0 {
			expected = response.TotalCount
		} else if response.TotalCount != expected {
			return nil, fmt.Errorf("GitHub Actions run total changed during pagination")
		}
		all = append(all, response.WorkflowRuns...)
		if len(all) >= expected {
			if len(all) != expected {
				return nil, fmt.Errorf("GitHub Actions run response was incomplete: expected %d, received %d", expected, len(all))
			}
			return all, nil
		}
		if len(response.WorkflowRuns) == 0 {
			return nil, fmt.Errorf("GitHub Actions run pagination was incomplete")
		}
	}
	return nil, fmt.Errorf("GitHub Actions run pagination exceeded safety limit")
}

func (c *GitHubClient) githubActionsJobs(ctx context.Context, repo string, runID int64) ([]githubActionsJob, error) {
	all := make([]githubActionsJob, 0)
	expected := -1
	for page := 1; page <= githubEvidenceMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?filter=latest&per_page=100&page=%d", url.PathEscape(c.owner), url.PathEscape(repo), runID, page)
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubActionsResponseLimit)
		if err != nil {
			return nil, err
		}
		if status == http.StatusForbidden {
			return nil, fmt.Errorf("GitHub Actions jobs fallback -> HTTP 403")
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("GitHub Actions jobs fallback -> HTTP %d", status)
		}
		var response githubActionsJobsResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			return nil, fmt.Errorf("decoding GitHub Actions jobs: %w", err)
		}
		if expected < 0 {
			expected = response.TotalCount
		} else if response.TotalCount != expected {
			return nil, fmt.Errorf("GitHub Actions job total changed during pagination")
		}
		all = append(all, response.Jobs...)
		if len(all) >= expected {
			if len(all) != expected {
				return nil, fmt.Errorf("GitHub Actions jobs response was incomplete: expected %d, received %d", expected, len(all))
			}
			return all, nil
		}
		if len(response.Jobs) == 0 {
			return nil, fmt.Errorf("GitHub Actions jobs pagination was incomplete")
		}
	}
	return nil, fmt.Errorf("GitHub Actions jobs pagination exceeded safety limit")
}

func (c *GitHubClient) requireGitHubEvidence(ctx context.Context, repo, base string, present map[string]struct{}, summary *githubCheckSummary) error {
	required, err := c.githubRequiredStatusChecks(ctx, repo, base)
	if errors.Is(err, errGitHubRequiredChecksUnavailable) {
		summary.EvidenceComplete = false
		summary.Lines = append(summary.Lines, "required_checks: unavailable")
		return nil
	}
	if err != nil {
		return err
	}
	for _, name := range required {
		if _, ok := present[name]; !ok {
			summary.EvidenceComplete = false
			summary.Lines = append(summary.Lines, "missing_required: "+name)
		}
	}
	return nil
}

func (c *GitHubClient) githubRequiredStatusChecks(ctx context.Context, repo, base string) ([]string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, nil
	}
	path := fmt.Sprintf("/repos/%s/%s/branches/%s/protection/required_status_checks", url.PathEscape(c.owner), url.PathEscape(repo), url.PathEscape(base))
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRepoMetadataResponseLimit)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status == http.StatusForbidden {
		return nil, errGitHubRequiredChecksUnavailable
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub required status checks -> HTTP %d", status)
	}
	var response githubRequiredStatusChecksResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return nil, fmt.Errorf("decoding GitHub required status checks: %w", err)
	}
	seen := make(map[string]struct{})
	for _, name := range response.Contexts {
		if name = strings.TrimSpace(name); name != "" {
			seen[name] = struct{}{}
		}
	}
	for _, check := range response.Checks {
		if name := strings.TrimSpace(check.Context); name != "" {
			seen[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func (c *GitHubClient) addGitHubCommitStatuses(ctx context.Context, repo, sha string, summary *githubCheckSummary) error {
	expected := -1
	processed := 0
	combinedState := ""
	for page := 1; page <= githubEvidenceMaxPages; page++ {
		path := fmt.Sprintf("/repos/%s/%s/commits/%s/status?per_page=100&page=%d", url.PathEscape(c.owner), url.PathEscape(repo), url.PathEscape(sha), page)
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubCommitStatusResponseLimit)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("GitHub commit status -> HTTP %d", status)
		}
		var response githubCombinedStatusResponse
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			return fmt.Errorf("decoding GitHub commit status: %w", err)
		}
		if page == 1 {
			combinedState = response.State
		}
		if expected < 0 {
			expected = response.TotalCount
			if expected == 0 && len(response.Statuses) > 0 {
				expected = len(response.Statuses)
			}
		} else if response.TotalCount != 0 && response.TotalCount != expected {
			return fmt.Errorf("GitHub commit status total changed during pagination")
		}
		for _, item := range response.Statuses {
			processed++
			summary.CommitStatuses++
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
		if processed >= expected {
			if processed != expected {
				return fmt.Errorf("GitHub commit status response was incomplete: expected %d, received %d", expected, processed)
			}
			break
		}
		if len(response.Statuses) == 0 {
			return fmt.Errorf("GitHub commit status pagination was incomplete")
		}
		if page == githubEvidenceMaxPages {
			return fmt.Errorf("GitHub commit status pagination exceeded safety limit")
		}
	}
	if summary.CommitStatuses > 0 {
		switch combinedState {
		case "success":
		case "pending":
			summary.Pending++
		case "failure", "error":
			summary.Failed++
		default:
			summary.Failed++
		}
	}
	return nil
}
