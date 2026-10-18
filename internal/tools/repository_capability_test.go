package tools

import "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"

var (
	_ catalog.RepositoryReadService  = (*RepositoryCapability)(nil)
	_ catalog.RepositoryWriteService = (*RepositoryCapability)(nil)
	_ catalog.MemoryService          = (*RepositoryCapability)(nil)
	_ catalog.HandoffService         = (*RepositoryCapability)(nil)
	_ catalog.NotesService           = (*RepositoryCapability)(nil)
)
