package tools

import (
	"errors"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// ManagedDeploymentCapability preserves the existing generic platform surface while
// routing the fixed production backend through the stricter catalog-aware rollout.
type ManagedDeploymentCapability struct {
	*PlatformCapability
}

func (c *ManagedDeploymentCapability) PlatformDeployPreview(appID string) (string, error) {
	appID = strings.TrimSpace(appID)
	if appID == managedBackendAppUUID {
		if err := c.requireMaintainerProfile(); err != nil {
			return "", err
		}
	}
	app, err := c.getPlatformApp(appID)
	if err != nil {
		return "", err
	}
	if app.UUID == managedBackendAppUUID {
		if err := c.requireMaintainerProfile(); err != nil {
			return "", err
		}
		sp := c.log.Start("platform_deploy_preview")
		result, err := c.managedBackendRolloutPreview(app)
		if err != nil {
			sp.Finish(audit.Deny, "managed backend preview", nil, err)
			return "", err
		}
		sp.Finish(audit.Allow, "managed backend preview", nil, nil)
		return result, nil
	}
	return c.PlatformCapability.PlatformDeployPreview(appID)
}

func (c *ManagedDeploymentCapability) PlatformDeploy(planID string, approve bool) (string, error) {
	plan, err := c.plans.Peek(strings.TrimSpace(planID))
	if err == nil && plan.Args["rollout"] == managedBackendRolloutMarker {
		if err := c.requireMaintainerProfile(); err != nil {
			return "", err
		}
		sp := c.log.Start("platform_deploy")
		needsApproval, policyErr := c.pol.CheckAction()
		if policyErr != nil {
			sp.Finish(audit.Deny, planID, nil, policyErr)
			return "", policyErr
		}
		if needsApproval && !approve {
			sp.Finish(audit.Ask, planID, nil, nil)
			return "APPROVAL REQUIRED: platform_deploy would dispatch the reviewed catalog-aware backend rollout. Re-invoke with approve=true.", nil
		}
		plan, err = c.plans.Consume(strings.TrimSpace(planID), "platform-deploy")
		if err != nil {
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		result, err := c.executeManagedBackendRollout(plan)
		if err != nil {
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		sp.Finish(audit.Allow, planID, nil, nil)
		return result, nil
	}
	return c.PlatformCapability.PlatformDeploy(planID, approve)
}

func (c *ManagedDeploymentCapability) PlatformDeployWithoutCachePreview(appID string) (string, error) {
	appID = strings.TrimSpace(appID)
	if appID == managedBackendAppUUID {
		return "", errors.New("managed backend force deployments are forbidden; use platform_deploy_preview")
	}
	app, err := c.getPlatformApp(appID)
	if err != nil {
		return "", err
	}
	if app.UUID == managedBackendAppUUID {
		return "", errors.New("managed backend force deployments are forbidden; use platform_deploy_preview")
	}
	return c.PlatformCapability.PlatformDeployWithoutCachePreview(appID)
}

func (c *ManagedDeploymentCapability) PlatformDeployWithoutCache(planID string, approve bool) (string, error) {
	if plan, err := c.plans.Peek(strings.TrimSpace(planID)); err == nil && plan.Args["app"] == managedBackendAppUUID {
		return "", errors.New("managed backend force deployments are forbidden")
	}
	return c.PlatformCapability.PlatformDeployWithoutCache(planID, approve)
}
