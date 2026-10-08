package catalog

import "encoding/json"

type SourceWorkflowService interface {
	SourceWorkflowDispatchPreview(repo, workflow, ref string, inputs map[string]string) (string, error)
	SourceWorkflowDispatch(planID string, approve bool) (string, error)
}

func RegisterSourceWorkflows(register Register, service SourceWorkflowService) {
	register(Tool{
		Name:        "source_workflow_dispatch_preview",
		Description: "Inspect one active owner-bound GitHub Actions workflow and exact branch SHA, then create a short-lived single-use dispatch plan. Nothing is triggered.",
		InputSchema: closedObject(map[string]any{
			"repo":     strProp("repository under configured owner"),
			"workflow": patternedStringProp("workflow file name under .github/workflows", `^[A-Za-z0-9][A-Za-z0-9._-]{0,126}\.ya?ml$`, 5, 132),
			"ref":      patternedStringProp("existing branch that contains the workflow", `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`, 1, 128),
			"inputs": map[string]any{
				"type":          "object",
				"description":   "bounded non-secret workflow_dispatch string inputs",
				"maxProperties": 25,
				"propertyNames": map[string]any{
					"pattern":   `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`,
					"maxLength": 64,
				},
				"additionalProperties": map[string]any{
					"type":      "string",
					"minLength": 1,
					"maxLength": 256,
					"pattern":   `^[A-Za-z0-9][A-Za-z0-9._:/+@-]{0,255}$`,
				},
			},
		}, "repo", "workflow", "ref"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var p struct {
				Repo     string            `json:"repo"`
				Workflow string            `json:"workflow"`
				Ref      string            `json:"ref"`
				Inputs   map[string]string `json:"inputs"`
			}
			if err := json.Unmarshal(arguments, &p); err != nil {
				return "", err
			}
			return service.SourceWorkflowDispatchPreview(p.Repo, p.Workflow, p.Ref, p.Inputs)
		},
	})
	register(Tool{
		Name:        "source_workflow_dispatch",
		Description: "Execute one reviewed source_workflow_dispatch_preview plan. Workflow state and exact branch SHA are revalidated; the token is never exposed.",
		InputSchema: closedObject(map[string]any{
			"plan_id": strProp("plan id returned by preview"),
			"approve": boolProp("dispatch when approval is required"),
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
			return service.SourceWorkflowDispatch(p.PlanID, p.Approve)
		},
	})
}
