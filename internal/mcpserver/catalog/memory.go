package catalog

import "encoding/json"

// MemoryService is the narrow domain contract required by structured repository
// memory tools. The concrete policy-backed service remains outside this package.
type MemoryService interface {
	MemoryReadIn(repo string) (string, error)
	MemoryWriteIn(repo, section, content string, approve bool) (string, error)
}

// RegisterMemory registers structured memory read/write tools in their original
// order. Handoff registration remains separate because it is not contiguous in the
// public catalog.
func RegisterMemory(register Register, service MemoryService) {
	register(Tool{
		Name:        "memory_read",
		Description: "Read the root or optional selected repo's agent-agnostic memory (.agent-memory/*.md), redacted.",
		InputSchema: object(map[string]any{"repo": strProp("optional repo directory, absolute or relative to the workspace root")}),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Repo string `json:"repo"`
			}
			_ = json.Unmarshal(arguments, &params)
			return service.MemoryReadIn(params.Repo)
		},
	})

	register(Tool{
		Name:        "memory_write",
		Description: "Write one structured memory section under .agent-memory/ (current-task, plan, decisions, reflections). Denied in read-only; in ask mode set approve=true. Content is redacted before persisting.",
		InputSchema: object(map[string]any{
			"section": strProp("one of: current-task, plan, decisions, reflections"),
			"content": strProp("Markdown memory content"),
			"approve": boolProp("write even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "section", "content"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Section string `json:"section"`
				Content string `json:"content"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.MemoryWriteIn(params.Repo, params.Section, params.Content, params.Approve)
		},
	})
}
