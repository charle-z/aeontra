package mcpserver

import "github.com/charle-z/mcp-devbox/internal/telemetry"

// WithTelemetry exposes the same durable aggregate store already attached to the
// observability sink. It does not create a second writer or alter MCP behavior.
func (s *Server) WithTelemetry(store *telemetry.Store) *Server {
	if s != nil {
		s.telemetry = store
	}
	return s
}
