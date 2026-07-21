package devaction

const (
	ToolClone          = "workspace_dev_git_clone"
	ToolPublishPreview = "workspace_dev_publish_preview"
	ToolPublish        = "workspace_dev_publish"
)

type Definition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func Definitions() []Definition {
	workspace := stringSchema("opaque registered development workspace id", `^ws_[a-f0-9]{32}$`, 35)
	simple := stringSchema("simple repository or directory name", `^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`, 100)
	branch := stringSchema("Git branch without refspecs or options", `^[A-Za-z0-9][A-Za-z0-9._/-]{0,126}[A-Za-z0-9]$`, 128)
	return []Definition{
		{Name: ToolClone, Description: "Clone one repository under the locally configured GitHub owner into a new simple directory inside this development workspace. Authentication remains Edge-only.", InputSchema: closedObject(map[string]any{
			"workspace_id": workspace, "repository": simple, "branch": branch, "directory": simple,
		}, []string{"workspace_id", "repository", "branch", "directory"})},
		{Name: ToolPublishPreview, Description: "Inspect one clean local repository and its exact GitHub branch, reject behind or diverged state, and create a short-lived single-use publication plan. It does not push.", InputSchema: closedObject(map[string]any{
			"workspace_id": workspace, "directory": simple, "branch": branch,
		}, []string{"workspace_id", "directory", "branch"})},
		{Name: ToolPublish, Description: "Execute one previously created development publication plan after revalidating repository, branch, HEAD and remote state. Never force-pushes, pushes tags, accepts a URL/refspec, or exposes credentials.", InputSchema: closedObject(map[string]any{
			"workspace_id": workspace,
			"plan_id":      stringSchema("opaque plan returned by workspace_dev_publish_preview", `^dp_[a-f0-9]{32}$`, 35),
		}, []string{"workspace_id", "plan_id"})},
	}
}

func closedObject(properties map[string]any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringSchema(description, pattern string, maxLength int) map[string]any {
	return map[string]any{"type": "string", "description": description, "pattern": pattern, "minLength": 1, "maxLength": maxLength}
}
