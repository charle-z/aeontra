package catalog

import "encoding/json"

// PlatformDeploymentService is the narrow contract required by normal and
// force-without-cache deployment planning and execution.
type PlatformDeploymentService interface {
	PlatformDeployPreview(app string) (string, error)
	PlatformDeployWithoutCachePreview(app string) (string, error)
	PlatformDeployWithoutCache(planID string, approve bool) (string, error)
}

// RegisterPlatformDeployment registers deployment planning and force execution in
// their original contiguous catalog order.
func RegisterPlatformDeployment(register Register, service PlatformDeploymentService) {
	register(Tool{
		Name:        "platform_deploy_preview",
		Description: "Read one allowed Coolify application and create an expiring single-use deployment plan bound to its repository, branch and expected commit. It does not deploy.",
		InputSchema: object(map[string]any{
			"app": strProp("Coolify application UUID"),
		}, "app"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformDeployPreview(params.App)
		},
	})

	register(Tool{
		Name:        "platform_deploy_without_cache_preview",
		Description: "Read one allowed Coolify application and create an expiring single-use force=true deployment plan bound to its repository, branch, and expected commit. It does not deploy.",
		InputSchema: object(map[string]any{
			"app": strProp("Coolify application UUID"),
		}, "app"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformDeployWithoutCachePreview(params.App)
		},
	})

	register(Tool{
		Name:        "platform_deploy_without_cache",
		Description: "Execute one reviewed platform_deploy_without_cache_preview plan after revalidating the application repository, branch, and expected commit. It requests Coolify force=true and requires explicit approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_without_cache_preview"),
			"approve": boolProp("execute the force=true deployment plan when approval is required"),
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
			return service.PlatformDeployWithoutCache(params.PlanID, params.Approve)
		},
	})
}
