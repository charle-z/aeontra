// Package catalog contains declarative MCP tool registrations grouped by domain.
// It intentionally does not depend on mcpserver.Server, policy, or integrations;
// callers provide the bounded handlers and retain all enforcement responsibilities.
package catalog

import "encoding/json"

// Handler is one MCP tool handler after JSON-RPC argument extraction.
type Handler func(json.RawMessage) (string, error)

// Tool is the stable registration contract consumed by mcpserver.Server.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Version     string
	Handler     Handler
}

// Register adds one declarative tool contract to the caller-owned registry.
type Register func(Tool)
