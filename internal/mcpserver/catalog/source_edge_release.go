package catalog

import "encoding/json"

type SourceEdgeReleaseService interface {
	SourceEdgeReleaseStatus() (string, error)
	SourceEdgeReleaseMaintenancePreview() (string, error)
	SourceEdgeReleaseMaintenanceApply(planID string, approve bool) (string, error)
}

func RegisterSourceEdgeRelease(register Register, service SourceEdgeReleaseService) {
	register(Tool{
		Name:        "source_edge_release_status",
		Description: "Read the repository maintainer profile's fixed edge-release environment, main protection/rules, release workflow runs/jobs, and release assets.",
		InputSchema: closedObject(map[string]any{}),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct{}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceEdgeReleaseStatus()
		},
	})
	register(Tool{
		Name:        "source_edge_release_maintenance_preview",
		Description: "Plan the fixed edge-release maintenance: cancel obsolete active release runs first, then allow only main with a custom deployment branch policy while leaving main unprotected.",
		InputSchema: closedObject(map[string]any{}),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct{}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceEdgeReleaseMaintenancePreview()
		},
	})
	register(Tool{
		Name:        "source_edge_release_maintenance_apply",
		Description: "Execute one reviewed fixed edge-release maintenance plan after revalidation. It cannot change branch protection or target another repository/environment.",
		InputSchema: closedObject(map[string]any{
			"plan_id": strProp("plan id returned by source_edge_release_maintenance_preview"),
			"approve": boolProp("apply when approval is required"),
		}, "plan_id"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceEdgeReleaseMaintenanceApply(p.PlanID, p.Approve)
		},
	})
}
