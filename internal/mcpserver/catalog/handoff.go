package catalog

import "encoding/json"

// HandoffService is the narrow contract required by the durable handoff tool.
type HandoffService interface {
	MemoryUpdateHandoff(content string) (string, error)
}

// RegisterHandoff registers the durable agent handoff tool at its original catalog
// position.
func RegisterHandoff(register Register, service HandoffService) {
	register(Tool{
		Name:        "memory_update_handoff",
		Description: "Write a handoff note into .agent-memory/handoffs/ so any agent can resume. Denied in read-only mode; content redacted.",
		InputSchema: object(map[string]any{"content": strProp("handoff note (Markdown)")}, "content"),
		Version:     "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.MemoryUpdateHandoff(params.Content)
		},
	})
}
