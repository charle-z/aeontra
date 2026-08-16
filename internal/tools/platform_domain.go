package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

var platformDomainLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type platformDomainSnapshot struct {
	UUID             string `json:"uuid"`
	Name             string `json:"name"`
	Repository       string `json:"repository"`
	Branch           string `json:"branch"`
	Commit           string `json:"commit"`
	BuildPack        string `json:"build_pack"`
	Dockerfile       string `json:"dockerfile"`
	PortsExposes     string `json:"ports_exposes"`
	AutoDeploy       bool   `json:"auto_deploy"`
	InstantDeploy    bool   `json:"instant_deploy"`
	HealthcheckPath  string `json:"healthcheck_path"`
	DestinationUUID  string `json:"destination_uuid"`
	DockerRunOptions string `json:"docker_run_options"`
}

func snapshotPlatformDomainConfiguration(app platformApplication) platformDomainSnapshot {
	return platformDomainSnapshot{
		UUID:             strings.TrimSpace(app.UUID),
		Name:             strings.TrimSpace(app.Name),
		Repository:       strings.TrimSpace(app.repo()),
		Branch:           strings.TrimSpace(app.branch()),
		Commit:           strings.TrimSpace(app.commit()),
		BuildPack:        strings.TrimSpace(app.BuildPack),
		Dockerfile:       strings.TrimSpace(app.Dockerfile),
		PortsExposes:     strings.TrimSpace(app.PortsExposes),
		AutoDeploy:       app.AutoDeploy,
		InstantDeploy:    app.InstantDeploy,
		HealthcheckPath:  strings.TrimSpace(app.HealthcheckPath),
		DestinationUUID:  strings.TrimSpace(app.DestinationUUID),
		DockerRunOptions: strings.TrimSpace(app.DockerRunOptions),
	}
}

func platformDomainConfigurationDigest(app platformApplication) (string, error) {
	body, err := json.Marshal(snapshotPlatformDomainConfiguration(app))
	if err != nil {
		return "", fmt.Errorf("encoding application configuration snapshot: %w", err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validatePlatformDomainHostname(host string) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || len(host) > 253 || net.ParseIP(host) != nil {
		return errors.New("domain hostname must be a DNS name")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return errors.New("domain hostname must be fully qualified")
	}
	for _, label := range labels {
		if !platformDomainLabelPattern.MatchString(label) {
			return errors.New("domain hostname contains an invalid label")
		}
	}
	return nil
}

func (s *PlatformCapability) normalizePlatformHTTPSDomain(raw string) (string, error) {
	if s.coolify == nil || len(s.coolify.allowedDomainRules) == 0 {
		return "", errors.New("COOLIFY_ALLOWED_DOMAINS is required for application domain updates")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("application domain must be one HTTPS origin without credentials, explicit port, query, fragment or path")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if err := validatePlatformDomainHostname(host); err != nil {
		return "", err
	}
	origin := "https://" + host
	if !s.coolify.domainAllowed(origin) {
		return "", fmt.Errorf("domain %q is not in COOLIFY_ALLOWED_DOMAINS", origin)
	}
	return origin, nil
}

func (s *PlatformCapability) requirePlatformDomainReady(app platformApplication) (managedApplicationDeployment, error) {
	if app.Status != "running:healthy" {
		return managedApplicationDeployment{}, fmt.Errorf("application is not running healthy: %s", app.Status)
	}
	deployment, err := s.latestManagedApplicationDeployment(app.UUID)
	if err != nil {
		return managedApplicationDeployment{}, err
	}
	if err := requireManagedDeployment("application", deployment, app.commit()); err != nil {
		return managedApplicationDeployment{}, err
	}
	return deployment, nil
}

func (s *PlatformCapability) PlatformAppDomainUpdatePreview(appID, rawDomain string) (string, error) {
	span := s.log.Start("platform_app_domain_update_preview")
	if err := s.coolify.builderConfigError(); err != nil {
		span.Finish(audit.Deny, "preview "+summarize(appID), nil, err)
		return "", err
	}
	targetDomain, err := s.normalizePlatformHTTPSDomain(rawDomain)
	if err != nil {
		span.Finish(audit.Deny, "preview "+summarize(appID), nil, err)
		return "", err
	}
	app, err := s.getPlatformApp(appID)
	if err != nil {
		span.Finish(audit.Deny, "preview "+summarize(appID), nil, err)
		return "", err
	}
	deployment, err := s.requirePlatformDomainReady(app)
	if err != nil {
		span.Finish(audit.Deny, "preview "+app.UUID, nil, err)
		return "", err
	}
	digest, err := platformDomainConfigurationDigest(app)
	if err != nil {
		span.Finish(audit.Error, "preview "+app.UUID, nil, err)
		return "", err
	}
	currentDomain := strings.TrimSpace(app.domain())
	changeRequired := currentDomain != targetDomain
	plan, err := s.plans.Create("platform-app-domain-update", map[string]string{
		"app": app.UUID, "current_domain": currentDomain, "target_domain": targetDomain,
		"configuration_digest": digest, "deployment_id": deployment.DeploymentUUID,
		"deployment_commit": deployment.Commit, "change_required": fmt.Sprint(changeRequired),
	})
	if err != nil {
		span.Finish(audit.Error, "preview "+app.UUID, nil, err)
		return "", err
	}
	span.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("app: %s\nname: %s\nrepository: %s\nbranch: %s\ncommit: %s\ncurrent_domain: %s\ntarget_domain: %s\nchange_required: %t\nlatest_deployment: %s\ndeployment_status: %s\neffect: PATCH only the application domains field with force_domain_override=false; no deployment is dispatched\nplan_id: %s\nexpiry: %s\n",
		app.UUID, app.Name, safePlatformURL(app.repo()), app.branch(), app.commit(), safePlatformURL(currentDomain), targetDomain,
		changeRequired, deployment.DeploymentUUID, deployment.Status, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) PlatformAppDomainUpdate(planID string, approve bool) (string, error) {
	span := s.log.Start("platform_app_domain_update")
	if err := s.coolify.builderConfigError(); err != nil {
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		span.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: platform_app_domain_update would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-app-domain-update")
	if err != nil {
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	targetDomain, err := s.normalizePlatformHTTPSDomain(plan.Args["target_domain"])
	if err != nil || targetDomain != plan.Args["target_domain"] {
		if err == nil {
			err = errors.New("domain policy changed after preview")
		}
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	app, err := s.getPlatformApp(plan.Args["app"])
	if err != nil {
		span.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	digest, err := platformDomainConfigurationDigest(app)
	if err != nil {
		span.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if app.UUID != plan.Args["app"] || strings.TrimSpace(app.domain()) != plan.Args["current_domain"] || digest != plan.Args["configuration_digest"] {
		err := errors.New("application changed after domain preview")
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	deployment, err := s.requirePlatformDomainReady(app)
	if err != nil {
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if deployment.DeploymentUUID != plan.Args["deployment_id"] || deployment.Commit != plan.Args["deployment_commit"] {
		err := errors.New("application deployment changed after domain preview")
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["change_required"] == "false" {
		span.Finish(audit.Allow, planID, nil, nil)
		return fmt.Sprintf("app: %s\nchanged: false\ndomain: %s\npreserved_configuration: true\ndeployment_dispatched: false\n", app.UUID, targetDomain), nil
	}
	if plan.Args["change_required"] != "true" {
		err := errors.New("domain update plan is invalid")
		span.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if patchErr := s.patchPlatformApplicationDomain(app.UUID, targetDomain); patchErr != nil {
		observed, observeErr := s.getPlatformApp(app.UUID)
		if observeErr != nil {
			unknown := fmt.Errorf("domain update request failed and its outcome could not be verified: %v; %w", patchErr, observeErr)
			span.Finish(audit.Error, planID, nil, unknown)
			return "", unknown
		}
		if strings.TrimSpace(observed.domain()) != plan.Args["current_domain"] {
			compensationErr := s.compensatePlatformApplicationDomain(app, plan.Args["current_domain"], plan.Args["configuration_digest"])
			if compensationErr != nil {
				combined := fmt.Errorf("domain update request failed after changing state and compensation failed: %v; %w", patchErr, compensationErr)
				span.Finish(audit.Error, planID, nil, combined)
				return "", combined
			}
			compensated := fmt.Errorf("domain update request failed after changing state and was compensated: %w", patchErr)
			span.Finish(audit.Error, planID, nil, compensated)
			return "", compensated
		}
		span.Finish(audit.Error, planID, nil, patchErr)
		return "", patchErr
	}
	updated, err := s.getPlatformApp(app.UUID)
	if err == nil {
		var updatedDigest string
		updatedDigest, err = platformDomainConfigurationDigest(updated)
		if err == nil && (strings.TrimSpace(updated.domain()) != targetDomain || updatedDigest != plan.Args["configuration_digest"] || updated.Status != "running:healthy") {
			err = errors.New("application configuration drifted during domain update")
		}
	}
	if err == nil {
		var updatedDeployment managedApplicationDeployment
		updatedDeployment, err = s.requirePlatformDomainReady(updated)
		if err == nil && (updatedDeployment.DeploymentUUID != plan.Args["deployment_id"] || updatedDeployment.Commit != plan.Args["deployment_commit"]) {
			err = errors.New("application deployment changed during domain update")
		}
	}
	if err != nil {
		compensationErr := s.compensatePlatformApplicationDomain(app, plan.Args["current_domain"], plan.Args["configuration_digest"])
		if compensationErr != nil {
			combined := fmt.Errorf("domain update verification failed and compensation failed: %v; %w", err, compensationErr)
			span.Finish(audit.Error, planID, nil, combined)
			return "", combined
		}
		compensated := fmt.Errorf("domain update verification failed and was compensated: %w", err)
		span.Finish(audit.Error, planID, nil, compensated)
		return "", compensated
	}
	span.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("app: %s\nchanged: true\ndomain: %s\npreserved_configuration: true\ndeployment_dispatched: false\n", app.UUID, targetDomain), nil
}

func (s *PlatformCapability) patchPlatformApplicationDomain(appID, domain string) error {
	status, body, err := s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(appID), map[string]any{
		"domains": domain, "force_domain_override": false,
	})
	if err != nil {
		return fmt.Errorf("updating application domain: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("updating application domain -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	return nil
}

func (s *PlatformCapability) compensatePlatformApplicationDomain(expected platformApplication, previousDomain, expectedDigest string) error {
	if err := s.patchPlatformApplicationDomain(expected.UUID, previousDomain); err != nil {
		return err
	}
	restored, err := s.getPlatformApp(expected.UUID)
	if err != nil {
		return err
	}
	digest, err := platformDomainConfigurationDigest(restored)
	if err != nil {
		return err
	}
	if strings.TrimSpace(restored.domain()) != previousDomain || digest != expectedDigest {
		return errors.New("compensated application does not match the previewed configuration")
	}
	return nil
}
