package catalog

import "encoding/json"

// PlatformCoreService is the narrow contract required by the legacy-compatible
// Coolify core tools and their reviewed plan execution paths.
type PlatformCoreService interface {
	PlatformDeploy(planID string, approve bool) (string, error)
	PlatformAppsList() (string, error)
	PlatformAppStatus(app string) (string, error)
	PlatformDeploymentStatus(deployment string) (string, error)
	PlatformAppLogs(app string, lines int) (string, error)
	PlatformAppCreate(planID string, approve bool) (string, error)
}

// RegisterPlatformCore registers the contiguous core Coolify tool block in its
// original catalog order.
func RegisterPlatformCore(register Register, service PlatformCoreService) {
	register(Tool{
		Name:        "coolify_deploy",
		Description: "Execute one previously reviewed platform_deploy_preview plan after revalidating the application repository, branch and expected commit. The plan is expiring and single-use; requires approval in ask mode; token is never exposed.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_preview"),
			"approve": boolProp("execute the deployment plan when approval is required"),
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
			return service.PlatformDeploy(params.PlanID, params.Approve)
		},
	})

	register(Tool{
		Name:        "coolify_list_apps",
		Description: "List applications on the configured Coolify instance. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set. Token is never exposed.",
		InputSchema: object(map[string]any{}),
		Version:     "1",
		Handler:     func(json.RawMessage) (string, error) { return service.PlatformAppsList() },
	})

	register(Tool{
		Name:        "coolify_app_status",
		Description: "Read one Coolify application by uuid. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced when configured.",
		InputSchema: object(map[string]any{"app": strProp("Coolify application uuid")}, "app"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformAppStatus(params.App)
		},
	})

	register(Tool{
		Name:        "coolify_deployment_status",
		Description: "Read one Coolify deployment by UUID and return a safe summary containing status, commit, timestamps, and application name. Token is never exposed.",
		InputSchema: object(map[string]any{"deployment": strProp("Coolify deployment UUID")}, "deployment"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Deployment string `json:"deployment"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformDeploymentStatus(params.Deployment)
		},
	})

	register(Tool{
		Name:        "coolify_app_logs",
		Description: "Read the latest bounded application logs from Coolify. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced and secrets are redacted.",
		InputSchema: object(map[string]any{
			"app":   strProp("Coolify application uuid"),
			"lines": map[string]any{"type": "integer", "description": "number of log lines from the end, from 1 to 1000; defaults to 100"},
		}, "app"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App   string `json:"app"`
				Lines int    `json:"lines"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformAppLogs(params.App, params.Lines)
		},
	})

	register(Tool{
		Name:        "coolify_create_app",
		Description: "Execute one previously reviewed platform_app_create_preview plan using the configured server/project/environment. Repository owner, domain, build, port and healthcheck were validated; plan is expiring and single-use; requires approval in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_app_create_preview"),
			"approve": boolProp("execute the application creation plan when approval is required"),
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
			return service.PlatformAppCreate(params.PlanID, params.Approve)
		},
	})
}
