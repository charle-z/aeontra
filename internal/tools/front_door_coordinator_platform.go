package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const (
	managedFrontDoorCoordinatorName          = "mcp-devbox-front-door-coordinator-managed"
	managedFrontDoorCoordinatorBranch        = "main"
	managedFrontDoorCoordinatorDockerfile    = "/Dockerfile.front-door-coordinator"
	managedFrontDoorCoordinatorPort          = "8766"
	managedFrontDoorCoordinatorHealthPath    = "/readyz"
	managedFrontDoorCoordinatorLegacyPath    = "/healthz"
	managedFrontDoorCoordinatorDockerOptions = "--add-host host.docker.internal:host-gateway"
	managedFrontDoorCoordinatorStateName     = "mcp-devbox-front-door-coordinator-state"
	managedFrontDoorCoordinatorStateMount    = "/coordinator-state"
)

type PlatformFrontDoorCoordinatorRequest struct {
	ExpectedProtocol    string
	ExpectedCatalogHash string
}

func (s *PlatformCapability) managedFrontDoorCoordinatorCoolifyURL() (string, error) {
	if s == nil || s.coolify == nil {
		return "", errors.New("coolify client is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(s.coolify.baseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("configured Coolify URL is not a fixed origin")
	}
	switch parsed.Scheme {
	case "https":
		return strings.TrimRight(parsed.String(), "/"), nil
	case "http":
		if parsed.Port() == "" {
			return "", errors.New("HTTP Coolify control origin requires an explicit port for the private host gateway")
		}
		return "http+host-gateway://" + parsed.Host, nil
	default:
		return "", errors.New("configured Coolify URL scheme is unsupported")
	}
}

func (s *PlatformCapability) PlatformFrontDoorCoordinatorPreview(request PlatformFrontDoorCoordinatorRequest) (string, error) {
	sp := s.log.Start("platform_front_door_coordinator_preview")
	if err := s.frontDoorPlatformConfigError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	coordinatorCoolifyURL, err := s.managedFrontDoorCoordinatorCoolifyURL()
	if err != nil {
		sp.Finish(audit.Deny, "preview Coolify origin", nil, err)
		return "", err
	}
	if !frontDoorProtocolPattern.MatchString(strings.TrimSpace(request.ExpectedProtocol)) || !frontDoorCatalogPattern.MatchString(strings.TrimSpace(request.ExpectedCatalogHash)) {
		err := errors.New("front-door coordinator compatibility identity is invalid")
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	mainSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorCoordinatorBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(mainSHA) {
		err = errors.New("main branch returned an invalid commit")
		sp.Finish(audit.Error, "preview main", nil, err)
		return "", err
	}
	frontSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(frontSHA) {
		err = errors.New("stable front-door branch returned an invalid commit")
		sp.Finish(audit.Error, "preview front", nil, err)
		return "", err
	}
	front, frontExists, err := s.managedFrontDoorApp()
	if err != nil || !frontExists {
		if err == nil {
			err = errors.New("managed front-door application is absent")
		}
		sp.Finish(audit.Deny, "preview front", nil, err)
		return "", err
	}
	backend, err := s.managedBackendApp()
	if err != nil {
		sp.Finish(audit.Deny, "preview backend", nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil {
		sp.Finish(audit.Error, "preview coordinator", nil, err)
		return "", err
	}
	action := "create"
	appID := ""
	if exists {
		if err := s.validateManagedFrontDoorCoordinatorReconcileApp(app); err != nil {
			sp.Finish(audit.Deny, "preview coordinator", nil, err)
			return "", err
		}
		action = "reconcile"
		appID = app.UUID
	}
	plan, err := s.plans.Create("platform-front-door-coordinator", map[string]string{
		"action": action, "app": appID, "main_sha": mainSHA, "front_sha": frontSHA,
		"front_app": front.UUID, "backend_app": backend.UUID,
		"expected_protocol":     strings.TrimSpace(request.ExpectedProtocol),
		"expected_catalog_hash": strings.TrimSpace(request.ExpectedCatalogHash),
		"server_uuid":           s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"environment_uuid": s.coolify.environmentUUID, "environment_name": s.coolify.environmentName,
		"destination_uuid": s.coolify.destinationUUID, "github_app_uuid": s.coolify.githubAppUUID,
		"coordinator_coolify_url": coordinatorCoolifyURL,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview plan", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("action: %s\napplication_name: %s\napplication_uuid: %s\nrepository: %s\nbranch: %s\nbranch_sha: %s\ndockerfile_location: %s\nport: %s\ndomain: none\nauto_deploy: disabled\ninstant_deploy: disabled\ndocker_run_options: %s\npersistent_storage: %s:%s\nfront_application_uuid: %s\nbackend_application_uuid: %s\nfront_commit: %s\nexpected_protocol: %s\nexpected_catalog_hash: %s\neffect: create or reconcile one private coordinator worker; no domain or topology changes\nplan_id: %s\nexpiry: %s\n",
		action, managedFrontDoorCoordinatorName, appID, s.managedFrontDoorRepository(), managedFrontDoorCoordinatorBranch,
		mainSHA, managedFrontDoorCoordinatorDockerfile, managedFrontDoorCoordinatorPort, managedFrontDoorCoordinatorDockerOptions,
		managedFrontDoorCoordinatorStateName, managedFrontDoorCoordinatorStateMount, front.UUID, backend.UUID, frontSHA,
		strings.TrimSpace(request.ExpectedProtocol), strings.TrimSpace(request.ExpectedCatalogHash), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) PlatformFrontDoorCoordinatorCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_front_door_coordinator_create")
	if err := s.frontDoorPlatformConfigError(); err != nil {
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
		return "APPROVAL REQUIRED: platform_front_door_coordinator_create would execute the reviewed coordinator plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-front-door-coordinator")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["server_uuid"] != s.coolify.serverUUID || plan.Args["project_uuid"] != s.coolify.projectUUID ||
		plan.Args["environment_uuid"] != s.coolify.environmentUUID || plan.Args["environment_name"] != s.coolify.environmentName ||
		plan.Args["destination_uuid"] != s.coolify.destinationUUID || plan.Args["github_app_uuid"] != s.coolify.githubAppUUID {
		err := errors.New("front-door coordinator platform configuration changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	coordinatorCoolifyURL, err := s.managedFrontDoorCoordinatorCoolifyURL()
	if err != nil || coordinatorCoolifyURL != plan.Args["coordinator_coolify_url"] {
		if err == nil {
			err = errors.New("front-door coordinator Coolify origin changed after preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	mainSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorCoordinatorBranch)
	if err != nil || mainSHA != plan.Args["main_sha"] {
		err = errors.New("main branch changed after coordinator preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	frontSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorBranch)
	if err != nil || frontSHA != plan.Args["front_sha"] {
		err = errors.New("stable front-door branch changed after coordinator preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	front, frontExists, err := s.managedFrontDoorApp()
	if err != nil || !frontExists || front.UUID != plan.Args["front_app"] {
		if err == nil {
			err = errors.New("managed front-door application changed after coordinator preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	backend, err := s.managedBackendApp()
	if err != nil || backend.UUID != plan.Args["backend_app"] {
		if err == nil {
			err = errors.New("managed backend application changed after coordinator preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	coordinatorTarget, coordinatorRequestID, err := managedFrontDoorCoordinatorDispatch(app.Description, exists)
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	created := false
	if exists {
		if plan.Args["action"] != "reconcile" || app.UUID != plan.Args["app"] {
			err := errors.New("managed coordinator application changed after preview")
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		app, err = s.reconcileManagedFrontDoorCoordinatorApp(app)
		if err != nil {
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
	} else {
		if plan.Args["action"] != "create" {
			err := errors.New("managed coordinator application disappeared after preview")
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		app, err = s.createManagedFrontDoorCoordinatorApp()
		if err != nil {
			sp.Finish(audit.Error, planID, nil, err)
			return "", err
		}
		created = true
	}
	storage, err := s.ensureManagedFrontDoorCoordinatorStorage(app.UUID)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	vars := map[string]string{
		"COOLIFY_URL":                            coordinatorCoolifyURL,
		"COOLIFY_API_TOKEN":                      s.coolify.token,
		"MCP_FRONT_DOOR_COORDINATOR_APP_UUID":    app.UUID,
		"MCP_FRONT_DOOR_APP_UUID":                front.UUID,
		"MCP_FRONT_DOOR_BACKEND_APP_UUID":        backend.UUID,
		"MCP_FRONT_DOOR_EXPECTED_COMMIT":         frontSHA,
		"MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT": mainSHA,
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":       plan.Args["expected_protocol"],
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH":   plan.Args["expected_catalog_hash"],
		"MCP_FRONT_DOOR_COORDINATOR_TARGET":      string(frontdoorcoordinator.TargetIdle),
		"MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT":  managedFrontDoorCoordinatorStateMount,
		"MCP_FRONT_DOOR_COORDINATOR_ADDR":        "0.0.0.0:" + managedFrontDoorCoordinatorPort,
	}
	vars["MCP_FRONT_DOOR_COORDINATOR_TARGET"] = string(coordinatorTarget)
	if coordinatorRequestID != "" {
		vars["MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID"] = coordinatorRequestID
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, err := s.coolify.setEnvironmentVariables(context.Background(), app.UUID, vars, keys); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	status, body, err := s.coolify.deploy(context.Background(), app.UUID, false)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("coordinator deployment request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("coordinator deployment -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	deployment := decodePlatformDeployResponse(body)
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("application_uuid: %s\napplication_created: %t\ndomain: none\npersistent_storage: %s\nenvironment_variables_configured: %d\ndeployed: true\ndeployment_id: %s\ndeployment_status: %s\nexpected_commit: %s\n",
		app.UUID, created, storage, len(vars), deployment.DeploymentUUID, deployment.Status, mainSHA), nil
}

func (s *PlatformCapability) PlatformFrontDoorTransitionPreview(targetRaw string) (string, error) {
	sp := s.log.Start("platform_front_door_transition_preview")
	target, err := frontdoorcoordinator.ParseTarget(targetRaw)
	if err != nil || target == frontdoorcoordinator.TargetIdle {
		if err == nil {
			err = errors.New("transition target must be cutover or rollback")
		}
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("managed front-door coordinator is absent")
		}
		sp.Finish(audit.Deny, "preview coordinator", nil, err)
		return "", err
	}
	if err := s.validateManagedFrontDoorCoordinatorApp(app); err != nil {
		sp.Finish(audit.Deny, "preview coordinator", nil, err)
		return "", err
	}
	if _, err := s.verifyManagedFrontDoorCoordinatorStorage(app.UUID); err != nil {
		sp.Finish(audit.Deny, "preview storage", nil, err)
		return "", err
	}
	front, frontExists, err := s.managedFrontDoorApp()
	if err != nil || !frontExists {
		if err == nil {
			err = errors.New("managed front-door application is absent")
		}
		sp.Finish(audit.Deny, "preview front", nil, err)
		return "", err
	}
	backend, err := s.managedBackendApp()
	if err != nil {
		sp.Finish(audit.Deny, "preview backend", nil, err)
		return "", err
	}
	identity, err := s.verifyManagedFrontDoorCoordinatorRuntime(app, front, backend)
	if err != nil {
		sp.Finish(audit.Deny, "preview runtime contract", nil, err)
		return "", err
	}
	frontBackend, err := s.managedFrontDoorConfiguredBackend(front.UUID)
	if err != nil {
		sp.Finish(audit.Deny, "preview front backend", nil, err)
		return "", err
	}
	topology := frontdoorcoordinator.Topology{FrontDomain: front.domain(), FrontBackendURL: frontBackend, BackendDomains: backend.domain()}
	phase, done, err := frontdoorcoordinator.NextPhase(target, topology)
	if err != nil {
		sp.Finish(audit.Deny, "preview topology", nil, err)
		return "", err
	}
	published, present, err := frontdoorcoordinator.DecodePublishedStatus(app.Description)
	if err != nil {
		sp.Finish(audit.Deny, "preview status", nil, err)
		return "", err
	}
	action := "dispatch"
	if done {
		action = "noop"
	} else if present && (published.State == frontdoorcoordinator.StateQueued || published.State == frontdoorcoordinator.StateRunning || published.State == frontdoorcoordinator.StateCompensating) {
		if published.Target != target {
			err := errors.New("a different front-door transition is already active")
			sp.Finish(audit.Deny, "preview status", nil, err)
			return "", err
		}
		action = "observe"
	}
	plan, err := s.plans.Create("platform-front-door-transition", map[string]string{
		"action": action, "target": string(target), "coordinator_app": app.UUID,
		"front_app": front.UUID, "backend_app": backend.UUID, "front_domain": topology.FrontDomain,
		"front_backend": topology.FrontBackendURL, "backend_domains": topology.BackendDomains, "status_revision": fmt.Sprint(published.Revision),
		"coordinator_commit": identity.CoordinatorCommit, "main_commit": identity.MainCommit, "front_commit": identity.FrontCommit,
		"expected_protocol": identity.Protocol, "expected_catalog_hash": identity.CatalogHash,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview plan", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	headlineAction := action
	if target == frontdoorcoordinator.TargetRollback && action == "dispatch" {
		headlineAction = string(target)
	}
	return fmt.Sprintf("action: %s\ndisposition: %s\ntarget: %s\nphase: %s\ncoordinator_application_uuid: %s\nfront_application_uuid: %s\nbackend_application_uuid: %s\nfront_domain: %s\nfront_backend: %s\nbackend_domains: %s\ncurrent_state: %s\ncurrent_revision: %d\ncoordinator_commit: %s\nfront_commit: %s\nbackend_commit: %s\nexpected_protocol: %s\nexpected_catalog_hash: %s\neffect: %s\nplan_id: %s\nexpiry: %s\n",
		headlineAction, action, target, phase, app.UUID, front.UUID, backend.UUID, topology.FrontDomain, topology.FrontBackendURL, topology.BackendDomains,
		published.State, published.Revision, identity.CoordinatorCommit, identity.FrontCommit, identity.MainCommit, identity.Protocol, identity.CatalogHash,
		managedFrontDoorTransitionEffect(action), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) PlatformFrontDoorTransition(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_front_door_transition")
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: platform_front_door_transition would execute the reviewed coordinator dispatch plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-front-door-transition")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil || !exists || app.UUID != plan.Args["coordinator_app"] {
		if err == nil {
			err = errors.New("managed coordinator changed after transition preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	published, present, err := frontdoorcoordinator.DecodePublishedStatus(app.Description)
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if !present {
		published.Revision = 0
	}
	if fmt.Sprint(published.Revision) != plan.Args["status_revision"] {
		err := errors.New("managed coordinator status changed after transition preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	front, frontExists, err := s.managedFrontDoorApp()
	if err != nil || !frontExists || front.UUID != plan.Args["front_app"] {
		if err == nil {
			err = errors.New("managed front door changed after transition preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	backend, err := s.managedBackendApp()
	identity := managedFrontDoorCoordinatorIdentity{}
	frontBackend := ""
	if err == nil {
		identity, err = s.verifyManagedFrontDoorCoordinatorRuntime(app, front, backend)
	}
	if err == nil {
		frontBackend, err = s.managedFrontDoorConfiguredBackend(front.UUID)
	}
	if err != nil || identity.CoordinatorCommit != plan.Args["coordinator_commit"] || identity.MainCommit != plan.Args["main_commit"] || identity.FrontCommit != plan.Args["front_commit"] ||
		identity.Protocol != plan.Args["expected_protocol"] || identity.CatalogHash != plan.Args["expected_catalog_hash"] || backend.UUID != plan.Args["backend_app"] || front.domain() != plan.Args["front_domain"] || frontBackend != plan.Args["front_backend"] || backend.domain() != plan.Args["backend_domains"] {
		if err == nil {
			err = errors.New("managed topology changed after transition preview")
		}
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	switch plan.Args["action"] {
	case "noop":
		sp.Finish(audit.Allow, planID, nil, nil)
		return "action: noop\ntransition_dispatched: false\nreason: target topology already reached\n", nil
	case "observe":
		sp.Finish(audit.Allow, planID, nil, nil)
		return "action: observe\ntransition_dispatched: false\nreason: matching transition already active\n", nil
	case "dispatch":
	default:
		err := errors.New("invalid front-door transition action")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	vars := map[string]string{
		"MCP_FRONT_DOOR_COORDINATOR_TARGET":     plan.Args["target"],
		"MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID": planID,
	}
	keys := []string{"MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID", "MCP_FRONT_DOOR_COORDINATOR_TARGET"}
	if _, err := s.coolify.setEnvironmentVariables(context.Background(), app.UUID, vars, keys); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	status, body, err := s.coolify.deploy(context.Background(), app.UUID, false)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("front-door coordinator dispatch failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("front-door coordinator dispatch -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	deployment := decodePlatformDeployResponse(body)
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("action: dispatch\ntarget: %s\ntransition_dispatched: true\ncoordinator_application_uuid: %s\ndeployment_id: %s\ndeployment_status: %s\n", plan.Args["target"], app.UUID, deployment.DeploymentUUID, deployment.Status), nil
}

func (s *PlatformCapability) PlatformFrontDoorTransitionStatus() (string, error) {
	sp := s.log.Start("platform_front_door_transition_status")
	app, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("managed front-door coordinator is absent")
		}
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	front, frontExists, err := s.managedFrontDoorApp()
	if err != nil || !frontExists {
		if err == nil {
			err = errors.New("managed front-door application is absent")
		}
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	backend, err := s.managedBackendApp()
	if err != nil {
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	frontBackend, err := s.managedFrontDoorConfiguredBackend(front.UUID)
	if err != nil {
		sp.Finish(audit.Deny, "status front backend", nil, err)
		return "", err
	}
	published, present, err := frontdoorcoordinator.DecodePublishedStatus(app.Description)
	if err != nil {
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	if !present {
		published = frontdoorcoordinator.Status{Target: frontdoorcoordinator.TargetIdle, State: frontdoorcoordinator.StateIdle}
	}
	storage, storageErr := s.verifyManagedFrontDoorCoordinatorStorage(app.UUID)
	contract := "valid"
	if err := s.validateManagedFrontDoorCoordinatorApp(app); err != nil || storageErr != nil {
		contract = "invalid"
	}
	sp.Finish(audit.Allow, "status "+app.UUID, nil, nil)
	return fmt.Sprintf("coordinator_application_uuid: %s\ncoordinator_status: %s\ncoordinator_deployment_state: %s\ncontract: %s\npersistent_storage: %s\ntarget: %s\nrecovery_target: %s\nstate: %s\nphase: %s\nrevision: %d\ndeployment_id: %s\nreason: %s\nfront_domain: %s\nfront_backend: %s\nbackend_domains: %s\nupdated_at: %s\n",
		app.UUID, app.Status, app.DeploymentStatus, contract, storage, published.Target, published.RecoveryTarget, published.State, published.Phase,
		published.Revision, published.DeploymentID, published.Reason, front.domain(), frontBackend, backend.domain(), published.UpdatedAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) managedFrontDoorConfiguredBackend(appID string) (string, error) {
	entries, err := s.coolify.listEnvironmentVariables(context.Background(), appID)
	if err != nil {
		return "", err
	}
	var matched *coolifyEnvironmentVariable
	for _, entry := range entries {
		if entry.IsPreview || entry.Key != "MCP_FRONT_DOOR_BACKEND_URL" {
			continue
		}
		if matched != nil {
			return "", errors.New("managed front-door backend environment is ambiguous")
		}
		copy := entry
		matched = &copy
	}
	if matched == nil || !matched.IsLiteral || !matched.IsRuntime || matched.IsBuildtime {
		return "", errors.New("managed front-door backend environment metadata is outside the fixed contract")
	}
	value, err := frontdoorcoordinator.ManagedEnvironmentValue(matched.Comment, s.coolify.token, matched.Key, frontdoorcoordinator.FrontPublicOrigin, frontdoorcoordinator.BackendOrigin)
	if err != nil {
		return "", fmt.Errorf("managed front-door backend environment is outside the fixed contract: %w", err)
	}
	return value, nil
}

func managedFrontDoorTransitionEffect(action string) string {
	switch action {
	case "noop":
		return "no write; target topology is already present"
	case "observe":
		return "no write; inspect the existing durable coordinator transition"
	default:
		return "set one closed target on the private coordinator and trigger one normal coordinator deployment"
	}
}

func (s *PlatformCapability) managedFrontDoorCoordinatorApp() (platformApplication, bool, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		return platformApplication{}, false, err
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, false, fmt.Errorf("listing coordinator applications -> HTTP %d", status)
	}
	apps, err := decodePlatformApplications(body)
	if err != nil {
		return platformApplication{}, false, err
	}
	var matches []platformApplication
	for _, app := range apps {
		if app.Name == managedFrontDoorCoordinatorName {
			matches = append(matches, app)
		}
	}
	if len(matches) == 0 {
		return platformApplication{}, false, nil
	}
	if len(matches) != 1 || !coolifyUUIDRe.MatchString(matches[0].UUID) {
		return platformApplication{}, false, errors.New("managed front-door coordinator identity is ambiguous")
	}
	app, err := s.getManagedApplication(matches[0].UUID)
	return app, err == nil, err
}

func (s *PlatformCapability) getManagedApplication(appID string) (platformApplication, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID), nil)
	if err != nil {
		return platformApplication{}, err
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, fmt.Errorf("reading managed application -> HTTP %d", status)
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		return platformApplication{}, err
	}
	if app.UUID == "" {
		app.UUID = appID
	}
	return app, nil
}

func (s *PlatformCapability) validateManagedFrontDoorCoordinatorApp(app platformApplication) error {
	return s.validateManagedFrontDoorCoordinatorContract(app, false)
}

func (s *PlatformCapability) validateManagedFrontDoorCoordinatorReconcileApp(app platformApplication) error {
	return s.validateManagedFrontDoorCoordinatorContract(app, true)
}

func (s *PlatformCapability) validateManagedFrontDoorCoordinatorContract(app platformApplication, allowLegacy bool) error {
	healthPathValid := app.HealthcheckPath == managedFrontDoorCoordinatorHealthPath
	dockerOptionsValid := strings.TrimSpace(app.DockerRunOptions) == managedFrontDoorCoordinatorDockerOptions
	if allowLegacy {
		healthPathValid = healthPathValid || app.HealthcheckPath == managedFrontDoorCoordinatorLegacyPath
		dockerOptionsValid = dockerOptionsValid || strings.TrimSpace(app.DockerRunOptions) == ""
	}
	if app.UUID == "" || app.Name != managedFrontDoorCoordinatorName || !s.managedFrontDoorRepositoryMatches(app.repo()) ||
		app.branch() != managedFrontDoorCoordinatorBranch || app.BuildPack != "dockerfile" ||
		app.Dockerfile != managedFrontDoorCoordinatorDockerfile || app.PortsExposes != managedFrontDoorCoordinatorPort ||
		app.AutoDeploy || app.InstantDeploy || !healthPathValid || !dockerOptionsValid || app.domain() != "" {
		return errors.New("existing front-door coordinator does not match the private worker contract")
	}
	return nil
}

func (s *PlatformCapability) reconcileManagedFrontDoorCoordinatorApp(app platformApplication) (platformApplication, error) {
	if err := s.validateManagedFrontDoorCoordinatorReconcileApp(app); err != nil {
		return platformApplication{}, err
	}
	if app.HealthcheckPath == managedFrontDoorCoordinatorHealthPath && strings.TrimSpace(app.DockerRunOptions) == managedFrontDoorCoordinatorDockerOptions {
		return app, nil
	}
	payload := map[string]any{
		"custom_docker_run_options": managedFrontDoorCoordinatorDockerOptions,
		"health_check_enabled":      true, "health_check_type": "http", "health_check_scheme": "http",
		"health_check_method": "GET", "health_check_path": managedFrontDoorCoordinatorHealthPath,
		"health_check_port": 8766, "health_check_return_code": 200, "health_check_interval": 10,
		"health_check_timeout": 3, "health_check_retries": 12, "health_check_start_period": 20,
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(app.UUID), payload)
	if err != nil {
		return platformApplication{}, err
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, fmt.Errorf("reconciling coordinator readiness healthcheck -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	updated, err := s.getManagedApplication(app.UUID)
	if err != nil {
		return platformApplication{}, err
	}
	if err := s.validateManagedFrontDoorCoordinatorApp(updated); err != nil {
		return platformApplication{}, err
	}
	return updated, nil
}

func (s *PlatformCapability) createManagedFrontDoorCoordinatorApp() (platformApplication, error) {
	payload := map[string]any{
		"name": managedFrontDoorCoordinatorName, "server_uuid": s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"destination_uuid": s.coolify.destinationUUID, "git_repository": s.managedFrontDoorRepository(),
		"git_branch": managedFrontDoorCoordinatorBranch, "build_pack": "dockerfile",
		"dockerfile_location": managedFrontDoorCoordinatorDockerfile, "ports_exposes": managedFrontDoorCoordinatorPort,
		"ports_mappings": "", "autogenerate_domain": false, "is_auto_deploy_enabled": false,
		"instant_deploy": false, "custom_docker_run_options": managedFrontDoorCoordinatorDockerOptions, "health_check_enabled": true,
		"health_check_type": "http", "health_check_scheme": "http", "health_check_method": "GET",
		"health_check_path": managedFrontDoorCoordinatorHealthPath, "health_check_port": 8766,
		"health_check_return_code": 200, "health_check_interval": 10, "health_check_timeout": 3,
		"health_check_retries": 12, "health_check_start_period": 20,
	}
	if s.coolify.environmentUUID != "" {
		payload["environment_uuid"] = s.coolify.environmentUUID
	} else {
		payload["environment_name"] = s.coolify.environmentName
	}
	endpoint := "/api/v1/applications/public"
	if s.coolify.githubAppUUID != "" {
		endpoint = "/api/v1/applications/private-github-app"
		payload["github_app_uuid"] = s.coolify.githubAppUUID
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPost, endpoint, payload)
	if err != nil {
		return platformApplication{}, err
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, fmt.Errorf("creating managed coordinator -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil || !coolifyUUIDRe.MatchString(app.UUID) {
		return platformApplication{}, errors.New("created coordinator application identity is invalid")
	}
	return app, nil
}

func (s *PlatformCapability) listManagedFrontDoorCoordinatorStorages(appID string) ([]coolifyStorage, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID)+"/storages", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("listing coordinator storages -> HTTP %d", status)
	}
	if entries, err := decodeCoolifyCollection[coolifyStorage](body); err == nil {
		return entries, nil
	}
	var grouped struct {
		Persistent []coolifyStorage `json:"persistent_storages"`
		Files      []coolifyStorage `json:"file_storages"`
	}
	if err := json.Unmarshal([]byte(body), &grouped); err != nil {
		return nil, errors.New("unexpected coordinator storage response")
	}
	var entries []coolifyStorage
	for _, storage := range grouped.Persistent {
		storage.Type = "persistent"
		storage.Name = strings.TrimPrefix(storage.Name, appID+"-")
		entries = append(entries, storage)
	}
	for _, storage := range grouped.Files {
		storage.Type = "file"
		storage.Name = strings.TrimPrefix(storage.Name, appID+"-")
		entries = append(entries, storage)
	}
	return entries, nil
}

func (s *PlatformCapability) verifyManagedFrontDoorCoordinatorStorage(appID string) (string, error) {
	storages, err := s.listManagedFrontDoorCoordinatorStorages(appID)
	if err != nil {
		return "", err
	}
	exact := 0
	for _, storage := range storages {
		nameMatches := storage.Name == managedFrontDoorCoordinatorStateName
		mountMatches := storage.MountPath == managedFrontDoorCoordinatorStateMount
		if !nameMatches && !mountMatches {
			continue
		}
		if nameMatches && mountMatches && storage.Type == "persistent" {
			exact++
			continue
		}
		return "", errors.New("front-door coordinator storage conflicts with the fixed contract")
	}
	if exact != 1 {
		return "", errors.New("front-door coordinator persistent storage is absent or duplicated")
	}
	return managedFrontDoorCoordinatorStateName + ":" + managedFrontDoorCoordinatorStateMount, nil
}

func (s *PlatformCapability) ensureManagedFrontDoorCoordinatorStorage(appID string) (string, error) {
	if value, err := s.verifyManagedFrontDoorCoordinatorStorage(appID); err == nil {
		return value, nil
	}
	storages, err := s.listManagedFrontDoorCoordinatorStorages(appID)
	if err != nil {
		return "", err
	}
	for _, storage := range storages {
		if storage.Name == managedFrontDoorCoordinatorStateName || storage.MountPath == managedFrontDoorCoordinatorStateMount {
			return "", errors.New("front-door coordinator storage conflicts with the fixed contract")
		}
	}
	payload := map[string]any{"type": "persistent", "name": managedFrontDoorCoordinatorStateName, "mount_path": managedFrontDoorCoordinatorStateMount}
	status, _, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/"+url.PathEscape(appID)+"/storages", payload)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("creating coordinator storage -> HTTP %d", status)
	}
	return s.verifyManagedFrontDoorCoordinatorStorage(appID)
}
