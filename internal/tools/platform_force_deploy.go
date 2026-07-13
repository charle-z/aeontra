package tools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const platformDeployWithoutCacheOperation = "platform-deploy-without-cache"

// PlatformDeployWithoutCachePreview creates an exact, short-lived plan for one
// force=true Coolify deployment. It binds the plan to the current application UUID,
// repository, branch, and commit so the later execution cannot target changed state.
func (s *PlatformCapability) PlatformDeployWithoutCachePreview(appID string) (string, error) {
	sp := s.log.Start("platform_deploy_without_cache_preview")
	app, err := s.getPlatformApp(appID)
	if err != nil {
		sp.Finish(audit.Deny, "preview "+summarize(appID), nil, err)
		return "", err
	}
	plan, err := s.plans.Create(platformDeployWithoutCacheOperation, map[string]string{
		"app": app.UUID, "name": app.Name, "repository": app.repo(), "branch": app.branch(), "commit": app.commit(),
	})
	if err != nil {
		sp.Finish(audit.Error, "preview "+app.UUID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("app: %s\nname: %s\nrepository: %s\nbranch: %s\nexpected_commit: %s\nforce: true\neffect: rebuild and deploy the configured Coolify application without reusable build cache\nplan_id: %s\nexpiry: %s\n",
		app.UUID, app.Name, app.repo(), app.branch(), app.commit(), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

// PlatformDeployWithoutCache executes one reviewed force=true deployment plan. It
// retains the same policy, approval, allowlist, token handling, and state
// revalidation as the normal deployment flow.
func (s *PlatformCapability) PlatformDeployWithoutCache(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_deploy_without_cache")
	if err := s.coolify.configError(); err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: platform_deploy_without_cache would execute the reviewed single-use plan with Coolify force=true. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), platformDeployWithoutCacheOperation)
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	app, err := s.getPlatformApp(plan.Args["app"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if app.UUID != plan.Args["app"] || app.repo() != plan.Args["repository"] || app.branch() != plan.Args["branch"] || app.commit() != plan.Args["commit"] {
		err := fmt.Errorf("application changed after deployment preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	status, body, err := s.coolify.deploy(context.Background(), app.UUID, true)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("Coolify deployment without cache request failed: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		err := fmt.Errorf("Coolify deployment without cache -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return s.coolifySafe(body), err
	}
	result := decodePlatformDeployResponse(body)
	trimmedBody := strings.TrimSpace(body)
	sp.Finish(audit.Allow, planID, nil, nil)
	out := fmt.Sprintf("http_status: %d\nforce: true\ndeployment_id: %s\nstatus: %s\n", status, result.DeploymentUUID, result.Status)
	if result.Message != "" {
		out += "message: " + s.coolifySafe(result.Message) + "\n"
	}
	if trimmedBody == "" {
		out += "response_body: empty\n"
	} else if result.DeploymentUUID == "" && result.Status == "" && result.Message == "" {
		out += "response_body: " + s.coolifySafe(trimmedBody) + "\n"
	}
	return out, nil
}
