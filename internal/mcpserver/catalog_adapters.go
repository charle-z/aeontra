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

type frontDoorPlatformAdapter struct {
	service *tools.Service
}

func (adapter frontDoorPlatformAdapter) PlatformFrontDoorCreatePreview(request catalog.FrontDoorPlatformPreviewRequest) (string, error) {
	return adapter.service.PlatformFrontDoorCreatePreview(tools.PlatformFrontDoorRequest{
		Domain: request.Domain, BackendURL: request.BackendURL, ExpectedProtocol: request.ExpectedProtocol,
		ExpectedCatalogHash: request.ExpectedCatalogHash,
	})
}

func (adapter frontDoorPlatformAdapter) PlatformFrontDoorCreate(planID string, approve bool) (string, error) {
	return adapter.service.PlatformFrontDoorCreate(planID, approve)
}

func (adapter frontDoorPlatformAdapter) PlatformFrontDoorStatus() (string, error) {
	return adapter.service.PlatformFrontDoorStatus()
}
