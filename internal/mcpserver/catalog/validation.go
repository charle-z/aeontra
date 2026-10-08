package catalog

import "encoding/json"

// ValidationService is the narrow domain contract required by configured tests
// and private fixed-profile project validation tools.
type ValidationService interface {
	RunTestsIn(cwd string, extra ...string) (string, error)
	ValidationPreview(repo, profile string) (string, error)
	ValidationExecute(planID string, approve bool) (string, error)
}

// RegisterValidation registers configured tests and private project validation in
// the same contiguous order used by the monolithic catalog.
func RegisterValidation(register Register, service ValidationService) {
	register(Tool{
		Name:        "run_tests",
		Description: "Run the project's configured allowlisted test command inside the attested private L3 executor. Network is denied and the optional cwd is jailed under the workspace. Only administrator-selected allow mode enables execution; read-only and ask modes deny.",
		InputSchema: object(map[string]any{
			"extra": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "extra arguments appended to the test command",
			},
			"cwd": strProp("optional working directory, absolute or relative to the workspace root"),
		}),
		Version: "3",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Extra []string `json:"extra"`
				CWD   string   `json:"cwd"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.RunTestsIn(params.CWD, params.Extra...)
		},
	})

	register(Tool{
		Name:        "project_validation_preview",
		Description: "Preview one fixed Node/pnpm validation profile for a direct child repository. Profiles are pnpm-lockfile (generate lockfile and fetch, no lifecycle scripts) and pnpm-validate (offline frozen install, check, test, build). The public MCP never receives Docker access, shell input, or arbitrary command arguments.",
		InputSchema: object(map[string]any{
			"repo":    strProp("direct repository name under /repos"),
			"profile": strProp("one fixed profile: pnpm-lockfile or pnpm-validate"),
		}, "repo", "profile"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo    string `json:"repo"`
				Profile string `json:"profile"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.ValidationPreview(params.Repo, params.Profile)
		},
	})

	register(Tool{
		Name:        "project_validation_execute",
		Description: "Execute one unexpired project_validation_preview plan in the separately deployed private validation runner. The runner accepts only the reviewed profile and repo, starts a hardened ephemeral Node 22 container, and returns redacted bounded output. It is never a free terminal.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by project_validation_preview"),
			"approve": boolProp("execute the reviewed validation plan when approval is required"),
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
			return service.ValidationExecute(params.PlanID, params.Approve)
		},
	})
}
