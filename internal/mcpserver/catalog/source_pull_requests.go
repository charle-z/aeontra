package catalog

import "encoding/json"

type SourcePullRequestService interface {
	SourcePullRequestCreatePreview(repo, head, base, title, description string) (string, error)
	SourcePullRequestCreate(planID string, approve bool) (string, error)
	SourcePullRequestStatus(repo string, number int) (string, error)
	SourcePullRequestMergePreview(repo string, number int) (string, error)
	SourcePullRequestMerge(planID string, approve bool) (string, error)
	SourceDefaultBranchUpdatePreview(repo, branch string) (string, error)
	SourceDefaultBranchUpdate(planID string, approve bool) (string, error)
}

func RegisterSourcePullRequests(register Register, service SourcePullRequestService) {
	register(Tool{Name: "source_pull_request_create_preview", Description: "Inspect exact owner-bound GitHub branch SHAs and create a short-lived single-use plan for one non-draft pull request. Nothing is created.", InputSchema: closedObject(map[string]any{"repo": strProp("repository under configured owner"), "head": strProp("source branch"), "base": strProp("base branch"), "title": boundedStringProp("pull request title", 1, 256), "description": boundedStringProp("optional pull request body", 0, 8192)}, "repo", "head", "base", "title"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct{ Repo, Head, Base, Title, Description string }
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourcePullRequestCreatePreview(p.Repo, p.Head, p.Base, p.Title, p.Description)
	}})
	register(Tool{Name: "source_pull_request_create", Description: "Execute one reviewed source_pull_request_create_preview plan. Branch SHAs and duplicate PR state are revalidated; token is never exposed.", InputSchema: closedObject(map[string]any{"plan_id": strProp("plan id returned by preview"), "approve": boolProp("execute when approval is required")}, "plan_id"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			PlanID  string `json:"plan_id"`
			Approve bool   `json:"approve"`
		}
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourcePullRequestCreate(p.PlanID, p.Approve)
	}})
	register(Tool{Name: "source_pull_request_status", Description: "Read one owner-bound GitHub pull request and all check runs/status contexts for its exact head SHA. Output is bounded and redacted.", InputSchema: closedObject(map[string]any{"repo": strProp("repository under configured owner"), "number": integerProp("pull request number", 1, 1000000)}, "repo", "number"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			Repo   string `json:"repo"`
			Number int    `json:"number"`
		}
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourcePullRequestStatus(p.Repo, p.Number)
	}})
	register(Tool{Name: "source_pull_request_merge_preview", Description: "Require an open mergeable pull request with at least one check and no pending or failed checks, then create an exact single-use merge-commit plan. Nothing is merged.", InputSchema: closedObject(map[string]any{"repo": strProp("repository under configured owner"), "number": integerProp("pull request number", 1, 1000000)}, "repo", "number"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			Repo   string `json:"repo"`
			Number int    `json:"number"`
		}
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourcePullRequestMergePreview(p.Repo, p.Number)
	}})
	register(Tool{Name: "source_pull_request_merge", Description: "Execute one reviewed green pull-request merge plan using GitHub merge_method=merge. Head SHA, mergeability and checks are revalidated.", InputSchema: closedObject(map[string]any{"plan_id": strProp("plan id returned by merge preview"), "approve": boolProp("execute when approval is required")}, "plan_id"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			PlanID  string `json:"plan_id"`
			Approve bool   `json:"approve"`
		}
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourcePullRequestMerge(p.PlanID, p.Approve)
	}})
	register(Tool{Name: "source_default_branch_update_preview", Description: "Read the current default branch and exact target branch SHA, then create a short-lived plan to update the owner-bound repository default branch. Nothing changes.", InputSchema: closedObject(map[string]any{"repo": strProp("repository under configured owner"), "branch": strProp("existing branch to make default")}, "repo", "branch"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct{ Repo, Branch string }
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourceDefaultBranchUpdatePreview(p.Repo, p.Branch)
	}})
	register(Tool{Name: "source_default_branch_update", Description: "Execute one reviewed default-branch update plan after revalidating the exact target branch SHA. Token is never exposed.", InputSchema: closedObject(map[string]any{"plan_id": strProp("plan id returned by preview"), "approve": boolProp("execute when approval is required")}, "plan_id"), Version: "1", Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			PlanID  string `json:"plan_id"`
			Approve bool   `json:"approve"`
		}
		if err := json.Unmarshal(arguments, &p); err != nil {
			return "", err
		}
		return service.SourceDefaultBranchUpdate(p.PlanID, p.Approve)
	}})
}
