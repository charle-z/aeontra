package catalog

import "encoding/json"

// SourceRepoCreationService is the narrow contract required by reviewed source
// repository creation preview and execution.
type SourceRepoCreationService interface {
	SourceRepoCreate(planID string, approve bool) (string, error)
	SourceRepoCreatePreview(name, visibility, description string) (string, error)
}

// RegisterSourceRepoCreation preserves the historical catalog order: execution
// first, followed by its preview tool.
func RegisterSourceRepoCreation(register Register, service SourceRepoCreationService) {
	register(Tool{
		Name:        "github_create_repo",
		Description: "Execute a previously reviewed source_repo_create_preview plan to create one GitHub repository under the configured owner. The plan is exact, expiring and single-use; token is never exposed; requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by source_repo_create_preview"),
			"approve": boolProp("execute the create plan when approval is required"),
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
			return service.SourceRepoCreate(params.PlanID, params.Approve)
		},
	})

	register(Tool{
		Name:        "source_repo_create_preview",
		Description: "Check that a repository is absent under the configured GitHub owner and create a read-only, exact, expiring and single-use creation plan. Private is the default; public must be explicit. Nothing is created.",
		InputSchema: object(map[string]any{
			"name":        strProp("new repository name under the configured owner"),
			"visibility":  strProp("optional private or public visibility; defaults to configured private posture"),
			"description": strProp("optional repository description; redacted before planning"),
		}, "name"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Name        string `json:"name"`
				Visibility  string `json:"visibility"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.SourceRepoCreatePreview(params.Name, params.Visibility, params.Description)
		},
	})
}
