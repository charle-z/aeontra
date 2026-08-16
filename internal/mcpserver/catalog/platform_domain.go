package catalog

import "encoding/json"

// PlatformDomainService owns the reviewed domain-only mutation for one allowed
// Coolify application. It does not expose a generic application PATCH surface.
type PlatformDomainService interface {
	PlatformAppDomainUpdatePreview(app, domain string) (string, error)
	PlatformAppDomainUpdate(planID string, approve bool) (string, error)
}

// RegisterPlatformDomain registers one closed preview/execute pair. The target
// domain remains bounded by immutable server configuration.
func RegisterPlatformDomain(register Register, service PlatformDomainService) {
	register(Tool{
		Name:        "platform_app_domain_update_preview",
		Description: "Read one allowed healthy Coolify application and its finished deployment, validate one HTTPS origin against COOLIFY_ALLOWED_DOMAINS, and create an expiring single-use plan bound to the app's non-secret configuration. It does not mutate or deploy.",
		InputSchema: object(map[string]any{
			"app":    strProp("allowed Coolify application UUID"),
			"domain": strProp("single HTTPS origin restricted by COOLIFY_ALLOWED_DOMAINS"),
		}, "app", "domain"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				App    string `json:"app"`
				Domain string `json:"domain"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformAppDomainUpdatePreview(params.App, params.Domain)
		},
	})

	register(Tool{
		Name:        "platform_app_domain_update",
		Description: "Consume and revalidate one platform_app_domain_update_preview plan, PATCH only domains with force_domain_override=false, verify all bound non-secret configuration was preserved, and compensate to the previous domain on verification drift. No deployment is dispatched; approval is required in ask mode.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_app_domain_update_preview"),
			"approve": boolProp("execute the domain update plan when approval is required"),
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
			return service.PlatformAppDomainUpdate(params.PlanID, params.Approve)
		},
	})
}
