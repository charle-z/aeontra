package tools

import "github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"

func managedFrontDoorCoordinatorDispatch(description string, exists bool) (frontdoorcoordinator.Target, string, error) {
	if !exists {
		return frontdoorcoordinator.TargetIdle, "", nil
	}
	published, present, err := frontdoorcoordinator.DecodePublishedStatus(description)
	if err != nil {
		return "", "", err
	}
	if !present {
		return frontdoorcoordinator.TargetIdle, "", nil
	}
	return published.Target, published.RequestID, nil
}
