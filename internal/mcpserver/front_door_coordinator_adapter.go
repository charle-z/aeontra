package mcpserver

import (
	"github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type frontDoorCoordinatorAdapter struct {
	service *tools.Service
}

func (adapter frontDoorCoordinatorAdapter) PlatformFrontDoorCoordinatorPreview(request catalog.FrontDoorCoordinatorPreviewRequest) (string, error) {
	return adapter.service.PlatformFrontDoorCoordinatorPreview(tools.PlatformFrontDoorCoordinatorRequest{
		ExpectedProtocol: request.ExpectedProtocol, ExpectedCatalogHash: request.ExpectedCatalogHash,
	})
}

func (adapter frontDoorCoordinatorAdapter) PlatformFrontDoorCoordinatorCreate(planID string, approve bool) (string, error) {
	return adapter.service.PlatformFrontDoorCoordinatorCreate(planID, approve)
}

func (adapter frontDoorCoordinatorAdapter) PlatformFrontDoorTransitionPreview(target string) (string, error) {
	return adapter.service.PlatformFrontDoorTransitionPreview(target)
}

func (adapter frontDoorCoordinatorAdapter) PlatformFrontDoorTransition(planID string, approve bool) (string, error) {
	return adapter.service.PlatformFrontDoorTransition(planID, approve)
}

func (adapter frontDoorCoordinatorAdapter) PlatformFrontDoorTransitionStatus() (string, error) {
	return adapter.service.PlatformFrontDoorTransitionStatus()
}
