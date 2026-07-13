package catalog

import "encoding/json"

// SourceRepoInfoService is the narrow contract required by source repository
// metadata lookup.
type SourceRepoInfoService interface {
	SourceRepoInfo(name string) (string, error)
}

// RegisterSourceRepoInfo registers repository metadata lookup at its original
// catalog position.
func RegisterSourceRepoInfo(register Register, service SourceRepoInfoService) {
	register(Tool{
		Name:        "github_repo_info",
		Description: "Read basic metadata for a repository under the configured GitHub owner. Token is never exposed and output is redacted.",
		InputSchema: object(map[string]any{
			"name": strProp("repository name"),
		}, "name"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.SourceRepoInfo(params.Name)
		},
	})
}
