package catalog

import "encoding/json"

// GitFastForwardService is the narrow contract required by reviewed fast-forward
// planning and execution.
type GitFastForwardService interface {
	RepoFastForwardPreview(repo string) (string, error)
	RepoFastForward(planID string, approve bool) (string, error)
}

// RegisterGitFastForward registers safe fast-forward planning and execution in
// their original contiguous catalog order.
func RegisterGitFastForward(register Register, service GitFastForwardService) {
	register(Tool{
		Name:        "repo_fast_forward_preview",
		Description: "Create a read-only, short-lived, single-use plan for an exact clean-tree fast-forward of the current attached branch to its existing upstream tracking ref. It does not fetch or modify the repository.",
		InputSchema: object(map[string]any{
			"repo": strProp("repository directory, absolute or relative to the workspace root"),
		}, "repo"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RepoFastForwardPreview(params.Repo)
		},
	})

	register(Tool{
		Name:        "repo_fast_forward",
		Description: "Execute one previously reviewed, unexpired and unused fast-forward plan using exactly 'git merge --ff-only <upstream>'. Repository, branch, HEAD, target and clean state are revalidated; requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by repo_fast_forward_preview"),
			"approve": boolProp("execute the plan when approval is required"),
		}, "plan_id"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RepoFastForward(params.PlanID, params.Approve)
		},
	})
}
