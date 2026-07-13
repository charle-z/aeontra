package catalog

import "encoding/json"

// GitReadService is the narrow contract required by read-only Git status and diff
// tools.
type GitReadService interface {
	GitStatus(repo ...string) (string, error)
	GitDiffIn(repo string, args ...string) (string, error)
}

// RegisterGitReads registers read-only Git inspection tools in their original
// contiguous catalog order.
func RegisterGitReads(register Register, service GitReadService) {
	register(Tool{
		Name:        "git_status",
		Description: "Show git working-tree status (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos.",
		InputSchema: object(map[string]any{
			"repo": strProp("optional repo directory, absolute or relative to the workspace root"),
		}),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			_ = json.Unmarshal(arguments, &params)
			return service.GitStatus(params.Repo)
		},
	})

	register(Tool{
		Name:        "git_diff",
		Description: "Show a git diff (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos. Optional extra args (e.g. --staged or a pathspec).",
		InputSchema: object(map[string]any{
			"repo": strProp("optional repo directory, absolute or relative to the workspace root"),
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "extra git diff arguments",
			},
		}),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Args []string `json:"args"`
				Repo string   `json:"repo"`
			}
			_ = json.Unmarshal(arguments, &params)
			return service.GitDiffIn(params.Repo, params.Args...)
		},
	})
}
