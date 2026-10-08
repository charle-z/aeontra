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

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const maxGitHubWorkflowInputs = 25

var (
	safeGitHubWorkflowPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,126}\.ya?ml$`)
	safeGitHubWorkflowInputNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	safeGitHubWorkflowInputValuePattern = regexp.MustCompile(
		`^[A-Za-z0-9][A-Za-z0-9._:/+@-]{0,255}$`,
	)
)

type githubWorkflowResponse struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
}

type githubWorkflowDispatchResponse struct {
	WorkflowRunID int64  `json:"workflow_run_id"`
	RunURL        string `json:"run_url"`
	HTMLURL       string `json:"html_url"`
}

func (c *GitHubClient) workflow(ctx context.Context, repo, workflow string) (githubWorkflowResponse, error) {
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/actions/workflows/" + url.PathEscape(workflow)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRepoMetadataResponseLimit)
	if err != nil {
		return githubWorkflowResponse{}, err
	}
	if status < 200 || status >= 300 {
		return githubWorkflowResponse{}, fmt.Errorf("GitHub workflow lookup -> HTTP %d", status)
	}
	var response githubWorkflowResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return githubWorkflowResponse{}, fmt.Errorf("decoding GitHub workflow lookup: %w", err)
	}
	if response.ID < 1 || response.Path != ".github/workflows/"+workflow || response.State != "active" {
		return githubWorkflowResponse{}, fmt.Errorf("GitHub workflow is missing, inactive, or outside .github/workflows")
	}
	return response, nil
}

func (c *GitHubClient) dispatchWorkflow(ctx context.Context, repo, workflow, ref string, inputs map[string]string) (githubWorkflowDispatchResponse, error) {
	body, err := json.Marshal(map[string]any{"ref": ref, "inputs": inputs})
	if err != nil {
		return githubWorkflowDispatchResponse{}, err
	}
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/actions/workflows/" + url.PathEscape(workflow) + "/dispatches"
	status, responseBody, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubRefAndMergeResponseLimit)
	if err != nil {
		return githubWorkflowDispatchResponse{}, err
	}
	if status == http.StatusForbidden {
		return githubWorkflowDispatchResponse{}, fmt.Errorf("GitHub workflow dispatch requires Actions: Write")
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return githubWorkflowDispatchResponse{}, fmt.Errorf("GitHub workflow dispatch -> HTTP %d", status)
	}
	if strings.TrimSpace(responseBody) == "" {
		return githubWorkflowDispatchResponse{}, nil
	}
	var response githubWorkflowDispatchResponse
	if err := json.Unmarshal([]byte(responseBody), &response); err != nil {
		return githubWorkflowDispatchResponse{}, fmt.Errorf("decoding GitHub workflow dispatch: %w", err)
	}
	return response, nil
}

func validateWorkflowDispatchInput(repo, workflow, ref string, inputs map[string]string) (map[string]string, string, error) {
	repo = strings.TrimSpace(repo)
	workflow = strings.TrimSpace(workflow)
	ref = strings.TrimSpace(ref)
	if !safeCloneDir(repo) {
		return nil, "", fmt.Errorf("invalid GitHub repository name %q", repo)
	}
	if !safeGitHubWorkflowPattern.MatchString(workflow) {
		return nil, "", fmt.Errorf("invalid GitHub workflow file name")
	}
	if !safeGitHubRef(ref) {
		return nil, "", fmt.Errorf("invalid GitHub workflow ref")
	}
	if len(inputs) > maxGitHubWorkflowInputs {
		return nil, "", fmt.Errorf("workflow inputs exceed %d entries", maxGitHubWorkflowInputs)
	}

	normalized := make(map[string]string, len(inputs))
	keys := make([]string, 0, len(inputs))
	for rawName, rawValue := range inputs {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name != rawName || value != rawValue || !safeGitHubWorkflowInputNamePattern.MatchString(name) || !safeGitHubWorkflowInputValuePattern.MatchString(value) {
			return nil, "", fmt.Errorf("invalid workflow input name or value")
		}
		normalized[name] = value
		keys = append(keys, name)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+normalized[key])
	}
	return normalized, strings.Join(pairs, ","), nil
}

func (s *SourceCapability) SourceWorkflowDispatchPreview(repo, workflow, ref string, inputs map[string]string) (string, error) {
	sp := s.log.Start("source_workflow_dispatch_preview")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	normalized, summary, err := validateWorkflowDispatchInput(repo, workflow, ref, inputs)
	if err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	if _, redacted := s.pol.Redact(summary); redacted {
		err := fmt.Errorf("workflow inputs contain secret-like material")
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}

	repo = strings.TrimSpace(repo)
	workflow = strings.TrimSpace(workflow)
	ref = strings.TrimSpace(ref)
	ctx := context.Background()
	refSHA, err := s.github.branchSHA(ctx, repo, ref)
	if err != nil {
		return "", err
	}
	workflowInfo, err := s.github.workflow(ctx, repo, workflow)
	if err != nil {
		return "", err
	}
	encodedInputs, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	plan, err := s.plans.Create("source-workflow-dispatch", map[string]string{
		"repo": repo, "workflow": workflow, "ref": ref, "ref_sha": refSHA,
		"workflow_id": fmt.Sprintf("%d", workflowInfo.ID), "inputs": string(encodedInputs), "inputs_summary": summary,
	})
	if err != nil {
		return "", err
	}
	sp.Finish(audit.Allow, summarize("preview", repo, workflow, ref, plan.ID), nil, nil)
	return fmt.Sprintf("repository: %s/%s\nworkflow: %s\nworkflow_name: %s\nref: %s\nref_sha: %s\ninputs: %s\neffect: dispatch one GitHub Actions workflow run\nplan_id: %s\nexpiry: %s\n", s.github.owner, repo, workflow, s.redact(safeGitHubDiagnosticField(workflowInfo.Name, 256)), ref, refSHA, summary, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *SourceCapability) SourceWorkflowDispatch(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_workflow_dispatch")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_workflow_dispatch would execute the reviewed workflow plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-workflow-dispatch")
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	refSHA, err := s.github.branchSHA(ctx, plan.Args["repo"], plan.Args["ref"])
	if err != nil || refSHA != plan.Args["ref_sha"] {
		return "", fmt.Errorf("workflow ref changed after preview")
	}
	workflowInfo, err := s.github.workflow(ctx, plan.Args["repo"], plan.Args["workflow"])
	if err != nil || fmt.Sprintf("%d", workflowInfo.ID) != plan.Args["workflow_id"] {
		return "", fmt.Errorf("workflow state changed after preview")
	}
	var inputs map[string]string
	if err := json.Unmarshal([]byte(plan.Args["inputs"]), &inputs); err != nil {
		return "", fmt.Errorf("decoding reviewed workflow inputs")
	}
	response, err := s.github.dispatchWorkflow(ctx, plan.Args["repo"], plan.Args["workflow"], plan.Args["ref"], inputs)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, planID, nil, nil)

	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s/%s\nworkflow: %s\ndispatched: true\nref: %s\nref_sha: %s\ninputs: %s\n", s.github.owner, plan.Args["repo"], plan.Args["workflow"], plan.Args["ref"], refSHA, plan.Args["inputs_summary"])
	if response.WorkflowRunID > 0 {
		fmt.Fprintf(&b, "workflow_run_id: %d\n", response.WorkflowRunID)
	}
	if response.HTMLURL != "" {
		fmt.Fprintf(&b, "url: %s\n", response.HTMLURL)
	}
	return s.redact(b.String()), nil
}
