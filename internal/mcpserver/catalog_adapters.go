package mcpserver

import (
	"github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type platformAppPreviewAdapter struct {
	service *tools.Service
}

func (adapter platformAppPreviewAdapter) PlatformAppCreatePreview(request catalog.PlatformAppCreatePreviewRequest) (string, error) {
	return adapter.service.PlatformAppCreatePreview(tools.PlatformAppCreateRequest{
		Name: request.Name, GitHubRepo: request.GitHubRepo, Branch: request.Branch, Domain: request.Domain,
		Port: request.Port, BuildPack: request.BuildPack, HealthcheckPath: request.HealthcheckPath,
		HealthcheckInterval: request.HealthcheckInterval, HealthcheckTimeout: request.HealthcheckTimeout,
		RequiredEnv: request.RequiredEnv,
	})
}
