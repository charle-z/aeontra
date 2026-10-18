package catalog

import "encoding/json"

// GitPublicationService is the narrow contract required by reviewed repository
// publication preview and execution.
type GitPublicationService interface {
	RepoPublish(planID string, approve bool) (string, error)
	RepoPublishPreview(repo, remote, branch string) (string, error)
}

// RegisterGitPublication preserves the historical catalog order: execution first,
// followed by its preview tool.
func RegisterGitPublication(register Register, service GitPublicationService) {
	register(Tool{
		Name:        "git_push",
		Description: "Execute a previously reviewed repo_publish_preview plan for one local branch and one named owner-restricted remote. No force, mirror, tags, refspecs, URL remotes, or extra arguments are accepted; requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by repo_publish_preview"),
			"approve": boolProp("execute the publication plan when approval is required"),
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
			return service.RepoPublish(params.PlanID, params.Approve)
		},
	})

	register(Tool{
		Name:        "repo_publish_preview",
		Description: "Validate a clean attached current branch and one named credential-free GitHub remote, inspect the exact remote branch state, reject behind/diverged publication, and create a read-only expiring single-use push plan. It does not push.",
		InputSchema: object(map[string]any{
			"repo":   strProp("repository directory, absolute or relative to the workspace root"),
			"remote": strProp("remote name, defaults to origin; URLs and option-like names are rejected"),
			"branch": strProp("branch name, defaults to and must equal the current attached branch"),
		}, "repo"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo   string `json:"repo"`
				Remote string `json:"remote"`
				Branch string `json:"branch"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RepoPublishPreview(params.Repo, params.Remote, params.Branch)
		},
	})
}
