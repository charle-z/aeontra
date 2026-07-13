package catalog

import "encoding/json"

// GitCommitService is the narrow contract required by local repository commits.
type GitCommitService interface {
	GitCommitIn(repo, message string, approve bool) (string, error)
}

// RegisterGitCommit registers commit creation at its original catalog position.
func RegisterGitCommit(register Register, service GitCommitService) {
	register(Tool{
		Name:        "git_commit",
		Description: "Stage all changes and commit them in the root or optional selected repo. Write action: denied in read-only; in ask mode set approve=true. Does not push.",
		InputSchema: object(map[string]any{
			"message": strProp("commit message"),
			"approve": boolProp("commit even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "message"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Message string `json:"message"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.GitCommitIn(params.Repo, params.Message, params.Approve)
		},
	})
}
