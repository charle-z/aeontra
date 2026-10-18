package tools

import "github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"

type platformAppPreviewCapability interface {
	PlatformAppCreatePreview(PlatformAppCreateRequest) (string, error)
}

var (
	_ catalog.PlatformCoreService             = (*PlatformCapability)(nil)
	_ catalog.PlatformDeploymentService       = (*PlatformCapability)(nil)
	_ catalog.PlatformEnvironmentService      = (*PlatformCapability)(nil)
	_ catalog.ValidationRunnerPlatformService = (*PlatformCapability)(nil)
	_ platformAppPreviewCapability            = (*PlatformCapability)(nil)
)
