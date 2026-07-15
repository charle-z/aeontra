package mcpserver

import "encoding/json"

func (s *Server) addClientCapabilitiesTool() {
	const name = "mcp_client_capabilities"
	s.table[name] = toolEntry{
		def: toolDef{
			Name:        name,
			Description: "Return the current MCP session's safely allowlisted client capability snapshot.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
			Version: "1",
			Annotations: map[string]any{
				"readOnlyHint":    true,
				"destructiveHint": false,
				"idempotentHint":  true,
				"openWorldHint":   false,
			},
		},
		handler: func(_ json.RawMessage) (string, error) {
			return s.encodeClientCapabilities("internal")
		},
		sessionHandler: func(_ json.RawMessage, sessionKey string) (string, error) {
			return s.encodeClientCapabilities(sessionKey)
		},
	}
	s.order = append(s.order, name)
}

func (s *Server) encodeClientCapabilities(sessionKey string) (string, error) {
	encoded, err := json.Marshal(s.clients.Snapshot(sessionKey))
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
