package htbaction

const (
	ToolStatus                 = "workspace_htb_status"
	ToolAuthValidate           = "workspace_htb_auth_validate"
	ToolCommand                = "workspace_htb_command"
	ToolCommandSave            = "workspace_htb_command_save"
	ToolCommandCredentialStdin = "workspace_htb_command_with_credential_stdin"
	ToolSessionClose           = "workspace_htb_session_close"
)

type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func Definitions() []Definition {
	workspace := stringSchema("opaque registered HTB workspace id", `^ws_[a-f0-9]{32}$`, 35)
	session := stringSchema("opaque HTB session bound to one runtime, workspace, and target", `^hs_[a-f0-9]{32}$`, 35)
	timeout := map[string]any{"type": "integer", "minimum": 5, "maximum": 600}
	command := map[string]any{"type": "string", "minLength": 1, "maxLength": 16 << 10, "description": "remote command executed explicitly inside the authorized target-locked HTB or controlled CTF laboratory"}
	credential := closedObject(map[string]any{
		"source":        map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "description": "workspace-relative local credential artifact under loot, scans, or tmp"},
		"extract_after": map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "description": "local extraction prefix; the extracted value never enters the model or control plane"},
	}, []string{"source", "extract_after"})
	return []Definition{
		{
			Name:        ToolStatus,
			Description: "Return safe status for one explicitly authorized Hack The Box or controlled CTF Linux workspace. The target and sensitive local values remain Edge-only.",
			InputSchema: closedObject(map[string]any{"workspace_id": workspace}, []string{"workspace_id"}),
		},
		{
			Name:        ToolAuthValidate,
			Description: "Validate a locally held credential against the single target registered for an authorized Hack The Box or controlled CTF workspace. The secret is extracted and consumed only on Edge.",
			InputSchema: closedObject(map[string]any{
				"workspace_id":    workspace,
				"username":        map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "pattern": `^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`},
				"credential":      credential,
				"timeout_seconds": timeout,
			}, []string{"workspace_id", "username", "credential", "timeout_seconds"}),
		},
		{
			Name:        ToolCommand,
			Description: "Execute one explicit remote command inside an authorized target-locked Hack The Box or controlled CTF session. This is not a generic host shell and cannot select another target.",
			InputSchema: closedObject(map[string]any{
				"workspace_id":    workspace,
				"session_id":      session,
				"command":         command,
				"timeout_seconds": timeout,
			}, []string{"workspace_id", "session_id", "command", "timeout_seconds"}),
		},
		{
			Name:        ToolCommandSave,
			Description: "Execute one explicit command in an authorized target-locked HTB or CTF session and save stdout locally without returning its contents. Destinations are limited to loot, reports, or tmp.",
			InputSchema: closedObject(map[string]any{
				"workspace_id":    workspace,
				"session_id":      session,
				"command":         command,
				"save_output":     map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "description": "workspace-relative destination under loot, reports, or tmp"},
				"timeout_seconds": timeout,
			}, []string{"workspace_id", "session_id", "command", "save_output", "timeout_seconds"}),
		},
		{
			Name:        ToolCommandCredentialStdin,
			Description: "Execute one bounded remote command in an authorized target-locked HTB or CTF session while supplying the session credential only through local stdin. The credential is absent from schema, argv, environment, responses, logs, and audit.",
			InputSchema: closedObject(map[string]any{
				"workspace_id":    workspace,
				"session_id":      session,
				"command":         command,
				"timeout_seconds": timeout,
			}, []string{"workspace_id", "session_id", "command", "timeout_seconds"}),
		},
		{
			Name:        ToolSessionClose,
			Description: "Close and invalidate one target-locked authorized HTB or controlled CTF session while preserving local evidence and saved outputs.",
			InputSchema: closedObject(map[string]any{
				"workspace_id": workspace,
				"session_id":   session,
			}, []string{"workspace_id", "session_id"}),
		},
	}
}

func closedObject(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description, pattern string, maxLength int) map[string]any {
	return map[string]any{"type": "string", "description": description, "pattern": pattern, "minLength": 1, "maxLength": maxLength}
}
