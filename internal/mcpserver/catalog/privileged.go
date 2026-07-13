package catalog

import "encoding/json"

// PrivilegedService is the narrow domain contract required by administrator-
// enabled fixed privileged profiles.
type PrivilegedService interface {
	PrivilegedTaskPreview(repo, profile string, params map[string]string) (string, error)
	PrivilegedTaskExecute(planID string, approve bool) (string, error)
}

// RegisterPrivileged registers privileged preview and execution in their original
// contiguous catalog order.
func RegisterPrivileged(register Register, service PrivilegedService) {
	register(Tool{
		Name:        "privileged_task_preview",
		Description: "Preview one administrator-enabled, server-defined privileged profile. The client supplies only a profile name and narrow validated parameters, never an executable, argv, or shell string. Returns the exact command, jailed working directory, network/filesystem posture, effect, risk, short-lived plan id and expiry. Disabled by default.",
		InputSchema: object(map[string]any{
			"repo":    strProp("jailed repository directory when the selected profile applies to a repository"),
			"profile": strProp("one approved server-defined profile name"),
			"params": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "narrow profile parameters such as remote, branch, or allowlisted service name",
			},
		}, "profile"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo    string            `json:"repo"`
				Profile string            `json:"profile"`
				Params  map[string]string `json:"params"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PrivilegedTaskPreview(params.Repo, params.Profile, params.Params)
		},
	})

	register(Tool{
		Name:        "privileged_task_execute",
		Description: "Execute one unexpired unused privileged_task_preview plan after policy approval. The exact server-generated command, jailed cwd, timeout and profile remain fixed. Docker profiles fail securely when safe containment is unavailable; no free host terminal is exposed.",
		InputSchema: object(map[string]any{
			"plan_id": strProp("plan id returned by privileged_task_preview"),
			"approve": boolProp("execute the privileged profile when approval is required"),
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
			return service.PrivilegedTaskExecute(params.PlanID, params.Approve)
		},
	})
}
