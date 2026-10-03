package tools

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"

	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

// Runner-only API methods reuse the existing token-private source broker.
// They are not registered as public tools or generic HTTP operations.
var ErrDevelopmentRunnerSourceNotPublic = errors.New("development runner source is unavailable as exact public Git objects; source stays on Edge")

func (c *GitHubClient) developmentRunnerPublicSource(ctx context.Context, owner, repo, sha string) error {
	prefix := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, prefix, nil, githubRepoMetadataResponseLimit)
	var metadata githubPublicRepoResponse
	if err != nil || (status != http.StatusOK && status != http.StatusNotFound) {
		return errors.New("development runner public source metadata unavailable")
	}
	if status == http.StatusNotFound || (json.Unmarshal([]byte(body), &metadata) == nil && (metadata.Private || metadata.Visibility != "public" || !strings.EqualFold(metadata.FullName, owner+"/"+repo))) {
		return ErrDevelopmentRunnerSourceNotPublic
	}
	if json.Unmarshal([]byte(body), &metadata) != nil {
		return errors.New("development runner public source metadata invalid")
	}
	status, body, err = c.doJSONLimit(ctx, http.MethodGet, prefix+"/git/commits/"+url.PathEscape(sha), nil, githubRepoMetadataResponseLimit)
	var commit struct {
		SHA string `json:"sha"`
	}
	if err == nil && (status == http.StatusNotFound || (status == http.StatusOK && json.Unmarshal([]byte(body), &commit) == nil && commit.SHA != sha)) {
		return ErrDevelopmentRunnerSourceNotPublic
	}
	if err != nil || status != http.StatusOK || json.Unmarshal([]byte(body), &commit) != nil {
		return errors.New("development runner source SHA is not present in the exact public repository")
	}
	return nil
}

func (c *GitHubClient) developmentRunnerFailure(ctx context.Context, repo string, runID int64) (development.FailureClass, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?filter=latest&per_page=10", url.PathEscape(c.owner), url.PathEscape(repo), runID)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubActionsResponseLimit)
	var response githubActionsJobsResponse
	if err != nil || status != http.StatusOK || json.Unmarshal([]byte(body), &response) != nil || response.TotalCount != 1 || len(response.Jobs) != 1 {
		return "", errors.New("development runner failure metadata is unavailable")
	}
	for _, step := range response.Jobs[0].Steps {
		if step.Status != "completed" || step.Conclusion != "failure" {
			continue
		}
		switch step.Name {
		case "Execute exact profile":
			return development.FailureCode, nil
		case "Probe kernel and CI contracts":
			return development.FailureCapabilityMissing, nil
		case "Prepare disposable VM", "Set up pinned Go":
			return development.FailureDependencyMissing, nil
		case "Validate immutable request", "Fetch exact public Git objects", "Bind trusted receipt":
			return development.FailureReconciliationNeeded, nil
		default:
			return development.FailureExternalTransient, nil
		}
	}
	return development.FailureExternalTransient, nil
}

type developmentRunnerGitHubRun struct {
	ID         int64     `json:"id"`
	WorkflowID int64     `json:"workflow_id"`
	RunAttempt int       `json:"run_attempt"`
	HeadSHA    string    `json:"head_sha"`
	Path       string    `json:"path"`
	Event      string    `json:"event"`
	Title      string    `json:"display_title"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
	Repository struct {
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
	} `json:"repository"`
}

// The trusted workflow recomputes this digest from its actual fixed inputs before
// running any gate. A caller-asserted plan digest alone cannot attest those inputs.
func developmentRunnerExecutionDigest(request DevelopmentRunnerRequest, workflowSHA string, workflowID int64) string {
	digest := sha256.New()
	digest.Write([]byte("aeontra-development-runner-execution-v1\x00"))
	for _, field := range []string{request.EffectID, strings.TrimPrefix(request.PlanDigest, "sha256:"), request.SourceOwner, request.SourceRepo, request.SourceSHA, request.CommandProfile, workflowSHA, fmt.Sprint(workflowID)} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		digest.Write(length[:])
		digest.Write([]byte(field))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (c *GitHubClient) developmentRunnerFindRun(ctx context.Context, repo, sha string, workflowID int64, request DevelopmentRunnerRequest, effect workqueue.DevelopmentRunnerEffect) (developmentRunnerGitHubRun, bool, error) {
	prefix := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo)
	title := "aeontra-development-" + request.EffectID + "-" + strings.TrimPrefix(request.PlanDigest, "sha256:") + "-" + developmentRunnerExecutionDigest(request, sha, workflowID)
	matches := func(run developmentRunnerGitHubRun) bool {
		return run.ID > 0 && run.WorkflowID == workflowID && run.RunAttempt == 1 && run.HeadSHA == sha && run.Event == "workflow_dispatch" && run.Title == title && strings.Split(run.Path, "@")[0] == ".github/workflows/"+developmentRunnerWorkflow && strings.EqualFold(run.Repository.FullName, c.owner+"/"+repo) && !run.Repository.Private && !run.CreatedAt.Before(effect.StartedAt.Add(-time.Minute))
	}
	if effect.RunID > 0 {
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, fmt.Sprintf("%s/actions/runs/%d", prefix, effect.RunID), nil, githubActionsResponseLimit)
		var run developmentRunnerGitHubRun
		if err != nil || status != http.StatusOK || json.Unmarshal([]byte(body), &run) != nil || !matches(run) {
			return run, false, errors.New("development runner exact run identity could not be verified")
		}
		return run, true, nil
	}
	var found developmentRunnerGitHubRun
	for page := 1; page <= 3; page++ {
		query := url.Values{"head_sha": {sha}, "event": {"workflow_dispatch"}, "created": {">=" + effect.StartedAt.Add(-time.Minute).UTC().Format(time.RFC3339)}, "per_page": {"100"}, "page": {fmt.Sprint(page)}}
		status, body, err := c.doJSONLimit(ctx, http.MethodGet, fmt.Sprintf("%s/actions/workflows/%d/runs?%s", prefix, workflowID, query.Encode()), nil, githubActionsResponseLimit)
		var response struct {
			WorkflowRuns []developmentRunnerGitHubRun `json:"workflow_runs"`
		}
		if err != nil || status != http.StatusOK || json.Unmarshal([]byte(body), &response) != nil || len(response.WorkflowRuns) > 100 {
			return found, false, errors.New("development runner bounded run lookup failed")
		}
		for _, run := range response.WorkflowRuns {
			if matches(run) {
				if found.ID != 0 {
					return found, false, errors.New("development runner effect has ambiguous runs")
				}
				found = run
			}
		}
		if len(response.WorkflowRuns) < 100 {
			return found, found.ID > 0, nil
		}
	}
	return found, false, errors.New("development runner run lookup exceeded bounded window")
}

func (c *GitHubClient) developmentRunnerCancel(ctx context.Context, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/cancel", url.PathEscape(c.owner), url.PathEscape(repo), runID)
	status, _, err := c.doJSONLimit(ctx, http.MethodPost, path, nil, githubRefAndMergeResponseLimit)
	if err != nil || status != http.StatusAccepted && status != http.StatusConflict {
		return errors.New("development runner cancellation acknowledgement is uncertain")
	}
	return nil // A 202/409 is not terminal; the next reconciliation reads the run.
}

func (c *GitHubClient) developmentRunnerVerifySteps(ctx context.Context, repo string, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?filter=latest&per_page=10", url.PathEscape(c.owner), url.PathEscape(repo), runID)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubActionsResponseLimit)
	var response githubActionsJobsResponse
	if err != nil || status != http.StatusOK || json.Unmarshal([]byte(body), &response) != nil || response.TotalCount != 1 || len(response.Jobs) != 1 {
		return errors.New("development runner job identity is invalid")
	}
	job := response.Jobs[0]
	if job.Name != "Isolated development execution" || job.Status != "completed" || job.Conclusion != "success" {
		return errors.New("development runner job did not succeed")
	}
	required := map[string]bool{"Validate immutable request": false, "Prepare disposable VM": false, "Fetch exact public Git objects": false, "Probe kernel and CI contracts": false, "Execute exact profile": false, "Bind trusted receipt": false}
	for _, step := range job.Steps {
		if _, ok := required[step.Name]; ok {
			if required[step.Name] || step.Status != "completed" || step.Conclusion != "success" {
				return errors.New("development runner trusted gate did not succeed")
			}
			required[step.Name] = true
		}
	}
	for _, success := range required {
		if !success {
			return errors.New("development runner trusted gate is missing")
		}
	}
	return nil
}
