package tools

import "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"

var (
	_ catalog.GitReadService             = (*GitCapability)(nil)
	_ catalog.GitAcquisitionService      = (*GitCapability)(nil)
	_ catalog.GitFastForwardService      = (*GitCapability)(nil)
	_ catalog.GitPublicationService      = (*GitCapability)(nil)
	_ catalog.GitRemoteManagementService = (*GitCapability)(nil)
	_ catalog.GitCommitService           = (*GitCapability)(nil)
)
