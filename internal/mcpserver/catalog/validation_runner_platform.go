package catalog

import "encoding/json"

// ValidationRunnerPlatformService is the narrow contract required by the managed
// private validation-runner application creation workflow.
type ValidationRunnerPlatformService interface {
	PlatformValidationRunnerCreatePreview(branch string) (string, error)
	PlatformValidationRunnerCreate(planID string, approve bool) (string, error)
}

// RegisterValidationRunnerPlatform registers validation-runner planning and
// creation in their original contiguous catalog order.
func RegisterValidationRunnerPlatform(register Register, service ValidationRunnerPlatformService) {
	register(Tool{
		Name:        "platform_validation_runner_create_preview",
		Description: "Plan exactly one private Coolify validation-runner application using the administrator-configured destination and exact mount allowlist. It never deploys or accepts secret values.",
		InputSchema: object(map[string]any{
			"branch": strProp("source branch; defaults to main"),
		}),
		Version: "2",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Branch string `json:"branch"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformValidationRunnerCreatePreview(params.Branch)
		},
	})

	register(Tool{
		Name:        "platform_validation_runner_create",
		Description: "Execute one reviewed validation-runner creation plan. It creates one private, non-deployed Coolify application and configures only non-secret runtime variables; explicit approval is required.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_validation_runner_create_preview"),
			"approve": boolProp("execute the reviewed application creation plan"),
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
			return service.PlatformValidationRunnerCreate(params.PlanID, params.Approve)
		},
	})
}
