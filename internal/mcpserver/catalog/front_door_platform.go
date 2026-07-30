package catalog

import (
	"encoding/json"
)

type FrontDoorPlatformPreviewRequest struct {
	Domain              string
	BackendURL          string
	ExpectedProtocol    string
	ExpectedCatalogHash string
}

type FrontDoorPlatformService interface {
	PlatformFrontDoorCreatePreview(FrontDoorPlatformPreviewRequest) (string, error)
	PlatformFrontDoorCreate(planID string, approve bool) (string, error)
	PlatformFrontDoorStatus() (string, error)
}

func RegisterFrontDoorPlatform(register Register, service FrontDoorPlatformService) {
	register(Tool{
		Name:        "platform_front_door_create_preview",
		Version:     "1",
		Description: "Validate and plan one fixed independently deployed MCP Front Door application. Repository, stable branch, Dockerfile, port, healthcheck, mounts, deployment flags and Coolify destination are server-owned; the caller supplies only allowed topology and the exact backend compatibility identity.",
		InputSchema: object(map[string]any{
			"domain":                strProp("temporary HTTPS domain restricted by COOLIFY_ALLOWED_DOMAINS"),
			"backend_url":           strProp("fixed HTTPS backend origin restricted by COOLIFY_ALLOWED_DOMAINS"),
			"expected_protocol":     strProp("exact MCP protocol date exposed by the approved backend"),
			"expected_catalog_hash": strProp("exact sha256 catalog hash exposed by the approved backend"),
		}, "domain", "backend_url", "expected_protocol", "expected_catalog_hash"),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct {
				Domain              string `json:"domain"`
				BackendURL          string `json:"backend_url"`
				ExpectedProtocol    string `json:"expected_protocol"`
				ExpectedCatalogHash string `json:"expected_catalog_hash"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorCreatePreview(FrontDoorPlatformPreviewRequest{
				Domain: params.Domain, BackendURL: params.BackendURL, ExpectedProtocol: params.ExpectedProtocol,
				ExpectedCatalogHash: params.ExpectedCatalogHash,
			})
		},
	})
	register(Tool{
		Name:        "platform_front_door_create",
		Version:     "1",
		Description: "Execute one reviewed managed Front Door plan. It creates or reconciles only the fixed application, upserts exactly three non-secret variables, and deploys only when the pinned stable-branch commit is not already healthy.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_front_door_create_preview"),
			"approve": boolProp("execute the reviewed plan when approval is required"),
		}, "plan_id"),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorCreate(params.PlanID, params.Approve)
		},
	})
	register(Tool{
		Name:        "platform_front_door_status",
		Version:     "1",
		Description: "Read the fixed managed Front Door application by server-owned name and return bounded repository, branch, commit, domain, health and contract metadata without exposing environment values.",
		InputSchema: object(map[string]any{}),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct{}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorStatus()
		},
	})
}
