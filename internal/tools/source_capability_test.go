package tools

import "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"

var (
	_ catalog.SourceRepoCreationService = (*SourceCapability)(nil)
	_ catalog.SourceRepoInfoService     = (*SourceCapability)(nil)
)
