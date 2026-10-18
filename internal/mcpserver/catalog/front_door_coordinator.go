package catalog

import "encoding/json"

type FrontDoorCoordinatorPreviewRequest struct {
	ExpectedProtocol    string
	ExpectedCatalogHash string
}

type FrontDoorCoordinatorService interface {
	PlatformFrontDoorCoordinatorPreview(FrontDoorCoordinatorPreviewRequest) (string, error)
	PlatformFrontDoorCoordinatorCreate(planID string, approve bool) (string, error)
	PlatformFrontDoorTransitionPreview(target string) (string, error)
	PlatformFrontDoorTransition(planID string, approve bool) (string, error)
	PlatformFrontDoorTransitionStatus() (string, error)
}

func RegisterFrontDoorCoordinator(register Register, service FrontDoorCoordinatorService) {
	register(Tool{
		Name:        "platform_front_door_coordinator_preview",
		Version:     "1",
		Description: "Plan creation or reconciliation of the single private Front Door coordinator worker. The application identity, main branch, Dockerfile, port, no-domain contract, persistent journal mount, managed application UUIDs and Coolify destination are server-owned.",
		InputSchema: object(map[string]any{
			"expected_protocol":     strProp("exact MCP protocol date exposed by the approved backend"),
			"expected_catalog_hash": strProp("exact sha256 catalog hash exposed by the approved backend"),
		}, "expected_protocol", "expected_catalog_hash"),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct {
				ExpectedProtocol    string `json:"expected_protocol"`
				ExpectedCatalogHash string `json:"expected_catalog_hash"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorCoordinatorPreview(FrontDoorCoordinatorPreviewRequest{
				ExpectedProtocol: params.ExpectedProtocol, ExpectedCatalogHash: params.ExpectedCatalogHash,
			})
		},
	})
	register(Tool{
		Name:        "platform_front_door_coordinator_create",
		Version:     "1",
		Description: "Execute one reviewed plan for the private coordinator worker. It creates no public domain, attaches only its dedicated persistent journal, configures fixed server-owned variables and triggers one normal non-force deployment.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_front_door_coordinator_preview"),
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
			return service.PlatformFrontDoorCoordinatorCreate(params.PlanID, params.Approve)
		},
	})
	register(Tool{
		Name:        "platform_front_door_transition_preview",
		Version:     "1",
		Description: "Inspect the fixed backend, facade and durable coordinator journal, then plan exactly one closed topology target. The result is dispatch, observe or noop; it never changes domains.",
		InputSchema: object(map[string]any{
			"target": enumProp("desired fixed topology", "cutover", "rollback"),
		}, "target"),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct {
				Target string `json:"target"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorTransitionPreview(params.Target)
		},
	})
	register(Tool{
		Name:        "platform_front_door_transition",
		Version:     "1",
		Description: "Execute one reviewed transition plan. Only a dispatch action writes: it sets the closed target on the private coordinator and triggers one normal coordinator deployment. Domain changes are performed later by that independent durable worker.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by platform_front_door_transition_preview"),
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
			return service.PlatformFrontDoorTransition(params.PlanID, params.Approve)
		},
	})
	register(Tool{
		Name:        "platform_front_door_transition_status",
		Version:     "1",
		Description: "Read the private coordinator application, persistent-journal contract, published monotonic transition status and current fixed topology without exposing environment values or credentials.",
		InputSchema: object(map[string]any{}),
		Handler: func(raw json.RawMessage) (string, error) {
			var params struct{}
			if err := json.Unmarshal(raw, &params); err != nil {
				return "", err
			}
			return service.PlatformFrontDoorTransitionStatus()
		},
	})
}
