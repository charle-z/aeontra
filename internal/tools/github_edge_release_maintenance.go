package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	edgeReleaseRepo            = "mcp-devbox"
	edgeReleaseEnvironmentName = "edge-release"
	edgeReleaseBranch          = "main"
	edgeReleaseWorkflow        = "edge-release.yml"
)

type edgeReleaseEnvironmentState struct {
	Name            string `json:"name"`
	ProtectionRules []struct {
		Type              string `json:"type"`
		WaitTimer         int    `json:"wait_timer"`
		PreventSelfReview bool   `json:"prevent_self_review"`
		Reviewers         []any  `json:"reviewers"`
	} `json:"protection_rules"`
	DeploymentBranchPolicy *struct {
		ProtectedBranches    bool `json:"protected_branches"`
		CustomBranchPolicies bool `json:"custom_branch_policies"`
	} `json:"deployment_branch_policy"`
}

type edgeReleaseBranchPolicies struct {
	BranchPolicies []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"branch_policies"`
}

type edgeReleaseBranchRule struct {
	Type              string `json:"type"`
	RulesetID         int64  `json:"ruleset_id"`
	RulesetSourceType string `json:"ruleset_source_type"`
	RulesetSource     string `json:"ruleset_source"`
}

type edgeReleaseRun struct {
	ID         int64  `json:"id"`
	WorkflowID int64  `json:"workflow_id"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type edgeReleaseRuns struct {
	WorkflowRuns []edgeReleaseRun `json:"workflow_runs"`
}

type edgeReleaseJobs struct {
	Jobs []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Steps      []struct {
			Number     int    `json:"number"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"steps"`
	} `json:"jobs"`
}

type edgeReleasePendingDeployment struct {
	Environment struct {
		Name string `json:"name"`
	} `json:"environment"`
	WaitTimer             int   `json:"wait_timer"`
	CurrentUserCanApprove bool  `json:"current_user_can_approve"`
	Reviewers             []any `json:"reviewers"`
}

type edgeReleaseAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type edgeReleaseRelease struct {
	TagName         string             `json:"tag_name"`
	TargetCommitish string             `json:"target_commitish"`
	Draft           bool               `json:"draft"`
	Prerelease      bool               `json:"prerelease"`
	Immutable       bool               `json:"immutable"`
	Assets          []edgeReleaseAsset `json:"assets"`
}

type edgeReleaseSnapshot struct {
	MainSHA          string
	MainProtected    bool
	BranchRules      []edgeReleaseBranchRule
	Environment      edgeReleaseEnvironmentState
	Policies         edgeReleaseBranchPolicies
	PoliciesReadable bool
	Runs             []edgeReleaseRun
	Releases         []edgeReleaseRelease
	Stable           edgeReleaseRelease
	StableExists     bool
}

func (c *GitHubClient) edgeReleaseGet(ctx context.Context, path string, limit int64, dst any) (int, error) {
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, limit)
	if err != nil {
		return 0, err
	}
	if status >= 200 && status < 300 && dst != nil {
		if err := json.Unmarshal([]byte(body), dst); err != nil {
			return status, fmt.Errorf("decoding GitHub edge-release response: %w", err)
		}
	}
	return status, nil
}

func (c *GitHubClient) edgeReleaseMainProtected(ctx context.Context) (bool, error) {
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/branches/" + edgeReleaseBranch + "/protection"
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, nil)
	if err != nil {
		return false, err
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("GitHub main protection lookup -> HTTP %d", status)
	}
	return true, nil
}

func (c *GitHubClient) edgeReleaseBranchRules(ctx context.Context) ([]edgeReleaseBranchRule, error) {
	var rules []edgeReleaseBranchRule
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/rules/branches/" + edgeReleaseBranch
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, &rules)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub main rules lookup -> HTTP %d", status)
	}
	return rules, nil
}

func (c *GitHubClient) edgeReleaseEnvironmentConfig(ctx context.Context) (edgeReleaseEnvironmentState, error) {
	var environment edgeReleaseEnvironmentState
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/environments/" + edgeReleaseEnvironmentName
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, &environment)
	if err != nil {
		return environment, err
	}
	if status < 200 || status >= 300 || environment.Name != edgeReleaseEnvironmentName || environment.DeploymentBranchPolicy == nil {
		return environment, fmt.Errorf("GitHub edge-release environment lookup -> HTTP %d", status)
	}
	return environment, nil
}

func (c *GitHubClient) edgeReleasePolicies(ctx context.Context) (edgeReleaseBranchPolicies, bool, error) {
	var policies edgeReleaseBranchPolicies
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/environments/" + edgeReleaseEnvironmentName + "/deployment-branch-policies?per_page=100"
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, &policies)
	if err != nil {
		return policies, false, err
	}
	if status == http.StatusNotFound || status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return policies, false, nil
	}
	if status < 200 || status >= 300 {
		return policies, false, fmt.Errorf("GitHub deployment branch policies lookup -> HTTP %d", status)
	}
	return policies, true, nil
}

func (c *GitHubClient) edgeReleaseRuns(ctx context.Context) ([]edgeReleaseRun, error) {
	var runs edgeReleaseRuns
	query := url.Values{"branch": {edgeReleaseBranch}, "event": {"workflow_dispatch"}, "per_page": {"100"}}
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/actions/workflows/" + edgeReleaseWorkflow + "/runs?" + query.Encode()
	status, err := c.edgeReleaseGet(ctx, path, githubActionsResponseLimit, &runs)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub edge-release runs lookup -> HTTP %d", status)
	}
	return runs.WorkflowRuns, nil
}

func (c *GitHubClient) edgeReleaseReleases(ctx context.Context) ([]edgeReleaseRelease, error) {
	var releases []edgeReleaseRelease
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/releases?per_page=20"
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, &releases)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub releases lookup -> HTTP %d", status)
	}
	return releases, nil
}

func (c *GitHubClient) edgeReleaseStable(ctx context.Context) (edgeReleaseRelease, bool, error) {
	var release edgeReleaseRelease
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/releases/tags/stable"
	status, err := c.edgeReleaseGet(ctx, path, githubRepoMetadataResponseLimit, &release)
	if err != nil {
		return release, false, err
	}
	if status == http.StatusNotFound {
		return release, false, nil
	}
	if status < 200 || status >= 300 {
		return release, false, fmt.Errorf("GitHub stable release lookup -> HTTP %d", status)
	}
	return release, true, nil
}

func (c *GitHubClient) edgeReleaseSnapshot(ctx context.Context) (edgeReleaseSnapshot, error) {
	mainSHA, err := c.branchSHA(ctx, edgeReleaseRepo, edgeReleaseBranch)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	mainProtected, err := c.edgeReleaseMainProtected(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	rules, err := c.edgeReleaseBranchRules(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	environment, err := c.edgeReleaseEnvironmentConfig(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	policies, readable, err := c.edgeReleasePolicies(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	runs, err := c.edgeReleaseRuns(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	releases, err := c.edgeReleaseReleases(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	stable, stableExists, err := c.edgeReleaseStable(ctx)
	if err != nil {
		return edgeReleaseSnapshot{}, err
	}
	return edgeReleaseSnapshot{MainSHA: mainSHA, MainProtected: mainProtected, BranchRules: rules, Environment: environment, Policies: policies, PoliciesReadable: readable, Runs: runs, Releases: releases, Stable: stable, StableExists: stableExists}, nil
}

func edgeReleaseActive(status string) bool {
	switch status {
	case "waiting", "queued", "in_progress", "pending", "requested":
		return true
	default:
		return false
	}
}

func edgeReleaseStaleRuns(snapshot edgeReleaseSnapshot) []edgeReleaseRun {
	stale := make([]edgeReleaseRun, 0)
	for _, run := range snapshot.Runs {
		if edgeReleaseActive(run.Status) && run.HeadSHA != snapshot.MainSHA {
			stale = append(stale, run)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].ID < stale[j].ID })
	return stale
}

func edgeReleasePolicyFingerprint(snapshot edgeReleaseSnapshot) string {
	policy := snapshot.Environment.DeploymentBranchPolicy
	parts := []string{
		"main_protected=" + strconv.FormatBool(snapshot.MainProtected),
		"protected_branches=" + strconv.FormatBool(policy.ProtectedBranches),
		"custom_branch_policies=" + strconv.FormatBool(policy.CustomBranchPolicies),
		"policies_readable=" + strconv.FormatBool(snapshot.PoliciesReadable),
	}
	rules := make([]string, 0, len(snapshot.BranchRules))
	for _, rule := range snapshot.BranchRules {
		rules = append(rules, fmt.Sprintf("%s:%d:%s:%s", rule.Type, rule.RulesetID, rule.RulesetSourceType, rule.RulesetSource))
	}
	sort.Strings(rules)
	parts = append(parts, "rules="+strings.Join(rules, ","))
	protections := make([]string, 0, len(snapshot.Environment.ProtectionRules))
	for _, rule := range snapshot.Environment.ProtectionRules {
		protections = append(protections, fmt.Sprintf("%s:%d:%t:%d", rule.Type, rule.WaitTimer, rule.PreventSelfReview, len(rule.Reviewers)))
	}
	sort.Strings(protections)
	parts = append(parts, "protection_rules="+strings.Join(protections, ","))
	branches := make([]string, 0, len(snapshot.Policies.BranchPolicies))
	for _, branch := range snapshot.Policies.BranchPolicies {
		branches = append(branches, fmt.Sprintf("%d:%s", branch.ID, branch.Name))
	}
	sort.Strings(branches)
	parts = append(parts, "branch_policies="+strings.Join(branches, ","))
	return strings.Join(parts, "|")
}

func edgeReleaseStaleFingerprint(snapshot edgeReleaseSnapshot) string {
	parts := make([]string, 0)
	for _, run := range edgeReleaseStaleRuns(snapshot) {
		parts = append(parts, fmt.Sprintf("%d:%s:%s", run.ID, run.HeadSHA, run.Status))
	}
	return strings.Join(parts, ",")
}

func edgeReleaseWaitTimer(environment edgeReleaseEnvironmentState) int {
	for _, rule := range environment.ProtectionRules {
		if rule.Type == "wait_timer" {
			return rule.WaitTimer
		}
	}
	return 0
}

func edgeReleaseReviewerCount(environment edgeReleaseEnvironmentState) int {
	total := 0
	for _, rule := range environment.ProtectionRules {
		if rule.Type == "required_reviewers" {
			total += len(rule.Reviewers)
		}
	}
	return total
}

func (c *GitHubClient) edgeReleaseRun(ctx context.Context, runID int64) (edgeReleaseRun, error) {
	var run edgeReleaseRun
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", url.PathEscape(c.owner), edgeReleaseRepo, runID)
	status, err := c.edgeReleaseGet(ctx, path, githubActionsResponseLimit, &run)
	if err != nil {
		return run, err
	}
	if status < 200 || status >= 300 {
		return run, fmt.Errorf("GitHub edge-release run lookup -> HTTP %d", status)
	}
	workflow, err := c.workflow(ctx, edgeReleaseRepo, edgeReleaseWorkflow)
	if err != nil {
		return run, err
	}
	if run.WorkflowID != workflow.ID || run.HeadBranch != edgeReleaseBranch || run.Event != "workflow_dispatch" {
		return run, fmt.Errorf("workflow run is outside the fixed edge-release workflow/main boundary")
	}
	return run, nil
}

func (c *GitHubClient) edgeReleaseJobs(ctx context.Context, runID int64) (edgeReleaseJobs, error) {
	var jobs edgeReleaseJobs
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?filter=all&per_page=100", url.PathEscape(c.owner), edgeReleaseRepo, runID)
	status, err := c.edgeReleaseGet(ctx, path, githubActionsResponseLimit, &jobs)
	if err != nil {
		return jobs, err
	}
	if status < 200 || status >= 300 {
		return jobs, fmt.Errorf("GitHub edge-release jobs lookup -> HTTP %d", status)
	}
	return jobs, nil
}

func (c *GitHubClient) edgeReleasePending(ctx context.Context, runID int64) ([]edgeReleasePendingDeployment, error) {
	var pending []edgeReleasePendingDeployment
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/pending_deployments", url.PathEscape(c.owner), edgeReleaseRepo, runID)
	status, err := c.edgeReleaseGet(ctx, path, githubActionsResponseLimit, &pending)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub pending deployments lookup -> HTTP %d", status)
	}
	filtered := pending[:0]
	for _, deployment := range pending {
		if deployment.Environment.Name == edgeReleaseEnvironmentName {
			filtered = append(filtered, deployment)
		}
	}
	return filtered, nil
}

func (c *GitHubClient) edgeReleaseCancel(ctx context.Context, runID int64) error {
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/cancel", url.PathEscape(c.owner), edgeReleaseRepo, runID)
	status, _, err := c.doJSONLimit(ctx, http.MethodPost, path, nil, githubActionsResponseLimit)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted {
		return fmt.Errorf("GitHub edge-release cancel -> HTTP %d", status)
	}
	return nil
}

func (c *GitHubClient) edgeReleaseWaitInactive(ctx context.Context, runID int64) error {
	for attempt := 0; attempt < 20; attempt++ {
		run, err := c.edgeReleaseRun(ctx, runID)
		if err != nil {
			return err
		}
		if !edgeReleaseActive(run.Status) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("obsolete edge-release run %d did not leave active state", runID)
}

func (c *GitHubClient) edgeReleaseSetPolicy(ctx context.Context) error {
	body, err := json.Marshal(map[string]any{
		"wait_timer":          0,
		"prevent_self_review": false,
		"reviewers":           []any{},
		"deployment_branch_policy": map[string]any{
			"protected_branches":     false,
			"custom_branch_policies": true,
		},
	})
	if err != nil {
		return err
	}
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/environments/" + edgeReleaseEnvironmentName
	status, _, err := c.doJSONLimit(ctx, http.MethodPut, path, body, githubRepoMetadataResponseLimit)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GitHub edge-release environment update -> HTTP %d", status)
	}
	return nil
}

func (c *GitHubClient) edgeReleaseCreateMainPolicy(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"name": edgeReleaseBranch})
	path := "/repos/" + url.PathEscape(c.owner) + "/" + edgeReleaseRepo + "/environments/" + edgeReleaseEnvironmentName + "/deployment-branch-policies"
	status, _, err := c.doJSONLimit(ctx, http.MethodPost, path, body, githubRepoMetadataResponseLimit)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GitHub main deployment branch policy create -> HTTP %d", status)
	}
	return nil
}

func (c *GitHubClient) edgeReleaseDeletePolicy(ctx context.Context, id int64) error {
	path := fmt.Sprintf("/repos/%s/%s/environments/%s/deployment-branch-policies/%d", url.PathEscape(c.owner), edgeReleaseRepo, edgeReleaseEnvironmentName, id)
	status, _, err := c.doJSONLimit(ctx, http.MethodDelete, path, nil, githubRepoMetadataResponseLimit)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("GitHub deployment branch policy delete -> HTTP %d", status)
	}
	return nil
}

func (s *SourceCapability) SourceEdgeReleaseStatus() (string, error) {
	sp := s.log.Start("source_edge_release_status")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	ctx := context.Background()
	snapshot, err := s.github.edgeReleaseSnapshot(ctx)
	if err != nil {
		sp.Finish(audit.Error, "status", nil, err)
		return "", err
	}
	policy := snapshot.Environment.DeploymentBranchPolicy
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s/%s\nmain_sha: %s\nmain_protected: %t\nmain_rules: %d\nenvironment: %s\nwait_timer: %d\nreviewers: %d\nprotected_branches: %t\ncustom_branch_policies: %t\nbranch_policies_readable: %t\n", s.github.owner, edgeReleaseRepo, snapshot.MainSHA, snapshot.MainProtected, len(snapshot.BranchRules), edgeReleaseEnvironmentName, edgeReleaseWaitTimer(snapshot.Environment), edgeReleaseReviewerCount(snapshot.Environment), policy.ProtectedBranches, policy.CustomBranchPolicies, snapshot.PoliciesReadable)
	for _, rule := range snapshot.BranchRules {
		fmt.Fprintf(&b, "main_rule: type=%s ruleset_id=%d source_type=%s source=%s\n", rule.Type, rule.RulesetID, safeGitHubDiagnosticField(rule.RulesetSourceType, 80), safeGitHubDiagnosticField(rule.RulesetSource, 120))
	}
	for _, branch := range snapshot.Policies.BranchPolicies {
		fmt.Fprintf(&b, "deployment_branch_policy: id=%d name=%s\n", branch.ID, branch.Name)
	}
	for _, run := range snapshot.Runs {
		fmt.Fprintf(&b, "run: id=%d status=%s conclusion=%s head_sha=%s created_at=%s updated_at=%s\n", run.ID, run.Status, run.Conclusion, run.HeadSHA, run.CreatedAt, run.UpdatedAt)
		if !edgeReleaseActive(run.Status) && run.HeadSHA != snapshot.MainSHA {
			continue
		}
		pending, err := s.github.edgeReleasePending(ctx, run.ID)
		if err != nil {
			return "", err
		}
		for _, deployment := range pending {
			fmt.Fprintf(&b, "pending_environment: run=%d name=%s wait_timer=%d reviewers=%d current_user_can_approve=%t\n", run.ID, deployment.Environment.Name, deployment.WaitTimer, len(deployment.Reviewers), deployment.CurrentUserCanApprove)
		}
		jobs, err := s.github.edgeReleaseJobs(ctx, run.ID)
		if err != nil {
			return "", err
		}
		for _, job := range jobs.Jobs {
			fmt.Fprintf(&b, "job: run=%d name=%s status=%s conclusion=%s\n", run.ID, safeGitHubDiagnosticField(job.Name, 240), job.Status, job.Conclusion)
			for _, step := range job.Steps {
				fmt.Fprintf(&b, "step: run=%d number=%d name=%s status=%s conclusion=%s\n", run.ID, step.Number, safeGitHubDiagnosticField(step.Name, 240), step.Status, step.Conclusion)
			}
		}
	}
	for _, release := range snapshot.Releases {
		fmt.Fprintf(&b, "release: tag=%s target=%s immutable=%t draft=%t prerelease=%t assets=%d\n", release.TagName, release.TargetCommitish, release.Immutable, release.Draft, release.Prerelease, len(release.Assets))
		for _, asset := range release.Assets {
			fmt.Fprintf(&b, "asset: tag=%s name=%s size=%d digest=%s\n", release.TagName, safeGitHubDiagnosticField(asset.Name, 240), asset.Size, safeGitHubDiagnosticField(asset.Digest, 160))
		}
	}
	fmt.Fprintf(&b, "stable_exists: %t\n", snapshot.StableExists)
	if snapshot.StableExists {
		for _, asset := range snapshot.Stable.Assets {
			fmt.Fprintf(&b, "stable_asset: name=%s size=%d digest=%s\n", safeGitHubDiagnosticField(asset.Name, 240), asset.Size, safeGitHubDiagnosticField(asset.Digest, 160))
		}
	}
	sp.Finish(audit.Allow, "status", nil, nil)
	return s.redact(b.String()), nil
}

func (s *SourceCapability) SourceEdgeReleaseMaintenancePreview() (string, error) {
	sp := s.log.Start("source_edge_release_maintenance_preview")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	snapshot, err := s.github.edgeReleaseSnapshot(context.Background())
	if err != nil {
		return "", err
	}
	if snapshot.MainProtected {
		return "", fmt.Errorf("main is protected; this closed operation never changes branch protection")
	}
	plan, err := s.plans.Create("source-edge-release-maintenance", map[string]string{
		"main_sha":           snapshot.MainSHA,
		"policy_fingerprint": edgeReleasePolicyFingerprint(snapshot),
		"stale_fingerprint":  edgeReleaseStaleFingerprint(snapshot),
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s/%s\nenvironment: %s\nmain_sha: %s\nmain_protected: false\ncurrent_protected_branches: %t\ncurrent_custom_branch_policies: %t\ntarget_protected_branches: false\ntarget_custom_branch_policies: true\ntarget_branch_policy: main\ntarget_wait_timer: 0\ntarget_reviewers: 0\n", s.github.owner, edgeReleaseRepo, edgeReleaseEnvironmentName, snapshot.MainSHA, snapshot.Environment.DeploymentBranchPolicy.ProtectedBranches, snapshot.Environment.DeploymentBranchPolicy.CustomBranchPolicies)
	stale := edgeReleaseStaleRuns(snapshot)
	fmt.Fprintf(&b, "obsolete_active_runs: %d\n", len(stale))
	for _, run := range stale {
		fmt.Fprintf(&b, "cancel_before_policy: id=%d head_sha=%s status=%s\n", run.ID, run.HeadSHA, run.Status)
	}
	fmt.Fprintf(&b, "effect: cancel obsolete edge-release runs first, then reconcile only edge-release deployment policy; branch protection is never changed\nplan_id: %s\nexpiry: %s\n", plan.ID, plan.ExpiresAt.Format(time.RFC3339))
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return s.redact(b.String()), nil
}

func (s *SourceCapability) SourceEdgeReleaseMaintenanceApply(planID string, approve bool) (string, error) {
	sp := s.log.Start("source_edge_release_maintenance_apply")
	if err := s.github.configError(); err != nil {
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: source_edge_release_maintenance_apply would cancel reviewed obsolete release runs and reconcile the fixed edge-release policy. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "source-edge-release-maintenance")
	if err != nil {
		return "", err
	}
	ctx := context.Background()
	before, err := s.github.edgeReleaseSnapshot(ctx)
	if err != nil {
		return "", err
	}
	if before.MainProtected || before.MainSHA != plan.Args["main_sha"] || edgeReleasePolicyFingerprint(before) != plan.Args["policy_fingerprint"] || edgeReleaseStaleFingerprint(before) != plan.Args["stale_fingerprint"] {
		return "", fmt.Errorf("edge-release state changed after preview")
	}
	stale := edgeReleaseStaleRuns(before)
	for _, expected := range stale {
		run, err := s.github.edgeReleaseRun(ctx, expected.ID)
		if err != nil {
			return "", err
		}
		if run.HeadSHA != expected.HeadSHA || run.Status != expected.Status || run.HeadSHA == before.MainSHA || !edgeReleaseActive(run.Status) {
			return "", fmt.Errorf("obsolete edge-release run changed after preview")
		}
		if err := s.github.edgeReleaseCancel(ctx, run.ID); err != nil {
			return "", err
		}
		if err := s.github.edgeReleaseWaitInactive(ctx, run.ID); err != nil {
			return "", err
		}
	}
	mid, err := s.github.edgeReleaseSnapshot(ctx)
	if err != nil {
		return "", err
	}
	if mid.MainProtected || mid.MainSHA != plan.Args["main_sha"] || edgeReleasePolicyFingerprint(mid) != plan.Args["policy_fingerprint"] || len(edgeReleaseStaleRuns(mid)) != 0 {
		return "", fmt.Errorf("edge-release policy state changed while cancelling obsolete runs")
	}
	if err := s.github.edgeReleaseSetPolicy(ctx); err != nil {
		return "", err
	}
	policies, readable, err := s.github.edgeReleasePolicies(ctx)
	if err != nil || !readable {
		return "", fmt.Errorf("custom deployment branch policies are not readable after enabling them")
	}
	mainFound := false
	for _, policy := range policies.BranchPolicies {
		if policy.Name == edgeReleaseBranch {
			mainFound = true
		}
	}
	if !mainFound {
		if err := s.github.edgeReleaseCreateMainPolicy(ctx); err != nil {
			return "", err
		}
	}
	policies, readable, err = s.github.edgeReleasePolicies(ctx)
	if err != nil || !readable {
		return "", fmt.Errorf("deployment branch policies became unreadable")
	}
	for _, policy := range policies.BranchPolicies {
		if policy.Name != edgeReleaseBranch {
			if err := s.github.edgeReleaseDeletePolicy(ctx, policy.ID); err != nil {
				return "", err
			}
		}
	}
	final, err := s.github.edgeReleaseSnapshot(ctx)
	if err != nil {
		return "", err
	}
	finalPolicy := final.Environment.DeploymentBranchPolicy
	if final.MainSHA != before.MainSHA || final.MainProtected || finalPolicy.ProtectedBranches || !finalPolicy.CustomBranchPolicies || edgeReleaseWaitTimer(final.Environment) != 0 || edgeReleaseReviewerCount(final.Environment) != 0 || !final.PoliciesReadable || len(final.Policies.BranchPolicies) != 1 || final.Policies.BranchPolicies[0].Name != edgeReleaseBranch || len(edgeReleaseStaleRuns(final)) != 0 {
		return "", fmt.Errorf("edge-release maintenance did not converge to the exact target")
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("repository: %s/%s\nenvironment: %s\nmain_sha: %s\nmain_protected: false\nprotected_branches: false\ncustom_branch_policies: true\ndeployment_branch_policy: main\nwait_timer: 0\nreviewers: 0\nobsolete_runs_cancelled: %d\n", s.github.owner, edgeReleaseRepo, edgeReleaseEnvironmentName, final.MainSHA, len(stale)), nil
}
