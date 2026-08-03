package tools

import (
	"errors"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func managedFrontDoorCoordinatorDispatch(description string, exists bool) (frontdoorcoordinator.Target, string, error) {
	if !exists {
		return frontdoorcoordinator.TargetIdle, "", nil
	}
	catalogStatus, catalogPresent, err := catalogrollout.DecodePublishedStatus(description)
	if err != nil {
		return "", "", err
	}
	if catalogPresent {
		switch catalogStatus.State {
		case catalogrollout.StateQueued, catalogrollout.StateRunning, catalogrollout.StateCompensating:
			return "", "", errors.New("catalog-aware backend rollout is active")
		default:
			return frontdoorcoordinator.TargetIdle, "", nil
		}
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
