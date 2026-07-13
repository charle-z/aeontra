package catalog

import "encoding/json"

// PlatformEnvironmentService is the narrow contract required by environment
// mutation on one configured platform application.
type PlatformEnvironmentService interface {
	CoolifySetEnv(app string, vars map[string]string, approve bool) (string, error)
}

// RegisterPlatformEnvironment registers environment mutation at its original
// catalog position.
func RegisterPlatformEnvironment(register Register, service PlatformEnvironmentService) {
	register(Tool{
		Name:        "coolify_set_env",
		Description: "Set environment variables on one Coolify application. Values are sent to Coolify but redacted from output/audit. Denied in read-only; in ask mode set approve=true.",
		InputSchema: object(map[string]any{
			"app": strProp("Coolify application uuid"),
			"vars": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "environment variables to set",
			},
			"approve": boolProp("set env vars even when approval is required"),
		}, "app", "vars"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App     string            `json:"app"`
				Vars    map[string]string `json:"vars"`
				Approve bool              `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.CoolifySetEnv(params.App, params.Vars, params.Approve)
		},
	})
}
