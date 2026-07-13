package tools

import "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"

var (
	_ catalog.ExecutionService  = (*ExecutionCapability)(nil)
	_ catalog.ValidationService = (*ExecutionCapability)(nil)
	_ catalog.PrivilegedService = (*ExecutionCapability)(nil)
)
