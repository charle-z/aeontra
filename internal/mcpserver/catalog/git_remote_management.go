package catalog

import "encoding/json"

// GitRemoteManagementService is the narrow contract required by reviewed Git
// remote planning and application.
type GitRemoteManagementService interface {
	RepoRemotePreview(repo, remote, repository string) (string, error)
	RepoRemoteSet(planID string, approve bool) (string, error)
}

// RegisterGitRemoteManagement registers remote preview and application in their
// original contiguous catalog order.
func RegisterGitRemoteManagement(register Register, service GitRemoteManagementService) {
	register(Tool{
		Name:        "repo_remote_preview",
		Description: "Create a read-only, exact, expiring and single-use plan to add or update one named Git remote in a jailed repository. The destination must be credential-free and stay under configured GITHUB_OWNER.",
		InputSchema: object(map[string]any{
			"repo":       strProp("repository directory, absolute or relative to the workspace root"),
			"remote":     strProp("remote name, defaults to origin"),
			"repository": strProp("repository name under configured owner, or an allowed credential-free HTTPS/SSH GitHub URL"),
		}, "repo", "repository"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo       string `json:"repo"`
				Remote     string `json:"remote"`
				Repository string `json:"repository"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RepoRemotePreview(params.Repo, params.Remote, params.Repository)
		},
	})

	register(Tool{
		Name:        "repo_remote_set",
		Description: "Execute one reviewed repo_remote_preview plan. It revalidates the current remote state and runs exactly git remote add or git remote set-url; requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by repo_remote_preview"),
			"approve": boolProp("execute the remote plan when approval is required"),
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
			return service.RepoRemoteSet(params.PlanID, params.Approve)
		},
	})
}
