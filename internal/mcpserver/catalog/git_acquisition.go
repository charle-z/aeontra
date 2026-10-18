package catalog

import "encoding/json"

// GitAcquisitionService is the narrow contract required by external repository
// clone and remote fetch operations.
type GitAcquisitionService interface {
	GitClone(url, dir string, approve bool) (string, error)
	RepoFetch(repo, remote string, approve bool) (string, error)
}

// RegisterGitAcquisition registers clone and fetch in their original contiguous
// catalog order.
func RegisterGitAcquisition(register Register, service GitAcquisitionService) {
	register(Tool{
		Name:        "git_clone",
		Description: "Clone a Git repository into a new simple directory under the workspace root. No embedded credentials in URLs; target cannot escape the jail. Denied in read-only; in ask mode set approve=true.",
		InputSchema: object(map[string]any{
			"url":     strProp("remote Git URL, without embedded credentials"),
			"dir":     strProp("optional simple target directory name under the workspace root; inferred from URL when omitted"),
			"approve": boolProp("clone even when approval is required"),
		}, "url"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				URL     string `json:"url"`
				Dir     string `json:"dir"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.GitClone(params.URL, params.Dir, params.Approve)
		},
	})

	register(Tool{
		Name:        "repo_fetch",
		Description: "Fetch one named remote into one jailed Git repository by running exactly 'git fetch <remote>'. No refspecs or extra arguments are accepted. This external action updates local remote-tracking refs and requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"repo":    strProp("repository directory, absolute or relative to the workspace root"),
			"remote":  strProp("remote name, defaults to origin; option-like names are rejected"),
			"approve": boolProp("execute the fetch when approval is required"),
		}, "repo"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo    string `json:"repo"`
				Remote  string `json:"remote"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RepoFetch(params.Repo, params.Remote, params.Approve)
		},
	})
}
