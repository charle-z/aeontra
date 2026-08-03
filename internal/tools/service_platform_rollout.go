package tools

import (
	"errors"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// PlatformDeployPreview keeps the existing public tool contract but routes the
// fixed managed backend through the catalog-aware planner.
func (s *Service) PlatformDeployPreview(appID string) (string, error) {
	app, err := s.PlatformCapability.getPlatformApp(strings.TrimSpace(appID))
	if err != nil {
		return "", err
	}
	if app.UUID == managedBackendAppUUID {
		sp := s.log.Start("platform_deploy_preview")
		result, err := s.PlatformCapability.managedBackendRolloutPreview(app)
		if err != nil {
			sp.Finish(audit.Deny, "managed backend preview", nil, err)
			return "", err
		}
		sp.Finish(audit.Allow, "managed backend preview", nil, nil)
		return result, nil
	}
	return s.PlatformCapability.PlatformDeployPreview(appID)
}

// PlatformDeploy executes a reviewed generic plan unchanged, or the stricter
// catalog-aware path when the preview marked the fixed backend application.
func (s *Service) PlatformDeploy(planID string, approve bool) (string, error) {
	plan, err := s.plans.Peek(strings.TrimSpace(planID))
	if err == nil && plan.Args["rollout"] == managedBackendRolloutMarker {
		sp := s.log.Start("platform_deploy")
		needsApproval, policyErr := s.pol.CheckAction()
		if policyErr != nil {
			sp.Finish(audit.Deny, planID, nil, policyErr)
			return "", policyErr
		}
		if needsApproval && !approve {
			sp.Finish(audit.Ask, planID, nil, nil)
			return "APPROVAL REQUIRED: platform_deploy would dispatch the reviewed catalog-aware backend rollout. Re-invoke with approve=true.", nil
		}
		plan, err = s.plans.Consume(strings.TrimSpace(planID), "platform-deploy")
		if err != nil {
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		result, err := s.PlatformCapability.executeManagedBackendRollout(plan)
		if err != nil {
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		sp.Finish(audit.Allow, planID, nil, nil)
		return result, nil
	}
	return s.PlatformCapability.PlatformDeploy(planID, approve)
}

func (s *Service) PlatformDeployWithoutCachePreview(appID string) (string, error) {
	app, err := s.PlatformCapability.getPlatformApp(strings.TrimSpace(appID))
	if err != nil {
		return "", err
	}
	if app.UUID == managedBackendAppUUID {
		return "", errors.New("managed backend force deployments are forbidden; use platform_deploy_preview")
	}
	return s.PlatformCapability.PlatformDeployWithoutCachePreview(appID)
}

func (s *Service) PlatformDeployWithoutCache(planID string, approve bool) (string, error) {
	if plan, err := s.plans.Peek(strings.TrimSpace(planID)); err == nil && plan.Args["app"] == managedBackendAppUUID {
		return "", errors.New("managed backend force deployments are forbidden")
	}
	return s.PlatformCapability.PlatformDeployWithoutCache(planID, approve)
}
