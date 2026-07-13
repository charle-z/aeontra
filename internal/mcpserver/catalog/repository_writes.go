package catalog

import "encoding/json"

// RepositoryWriteService is the narrow domain contract required by patch-first
// repository write tools.
type RepositoryWriteService interface {
	ApplyPatchIn(repo, patch string, approve bool) (string, error)
	CreateFileIn(repo, path, content string, approve bool) (string, error)
}

// RegisterRepositoryWrites registers the contiguous patch and file creation tools
// in the same order used by the monolithic catalog.
func RegisterRepositoryWrites(register Register, service RepositoryWriteService) {
	register(Tool{
		Name:        "apply_patch",
		Description: "Apply a unified diff (patch-first). Optional repo makes patch paths relative to that jailed repo. Validated with 'git apply --check' first; targets jailed and secret-protected. In ask mode, set approve=true to apply after review.",
		InputSchema: object(map[string]any{
			"patch":   strProp("unified diff text"),
			"approve": boolProp("apply even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "patch"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Patch   string `json:"patch"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.ApplyPatchIn(params.Repo, params.Patch, params.Approve)
		},
	})

	register(Tool{
		Name:        "create_file",
		Description: "Create a NEW file (patch-first: built as a diff and validated; refuses to overwrite — use apply_patch to modify). Jailed and secret-protected. In ask mode set approve=true.",
		InputSchema: object(map[string]any{
			"path":    strProp("new file path relative to the project root"),
			"content": strProp("file content"),
			"approve": boolProp("create even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "path", "content"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.CreateFileIn(params.Repo, params.Path, params.Content, params.Approve)
		},
	})
}
