package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	managedFrontDoorName       = "mcp-devbox-front-door-managed"
	managedFrontDoorBranch     = "front-door-stable"
	managedFrontDoorDockerfile = "/Dockerfile.front-door"
	managedFrontDoorPort       = "8765"
	managedFrontDoorHealthPath = "/front-door/healthz"
)

var (
	frontDoorProtocolPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	frontDoorCatalogPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	frontDoorCommitPattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type PlatformFrontDoorRequest struct {
	Domain              string
	BackendURL          string
	ExpectedProtocol    string
	ExpectedCatalogHash string
}

func (s *PlatformCapability) PlatformFrontDoorCreatePreview(request PlatformFrontDoorRequest) (string, error) {
	sp := s.log.Start("platform_front_door_create_preview")
	if err := s.frontDoorPlatformConfigError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	normalized, err := s.normalizeFrontDoorRequest(request)
	if err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	sha, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorBranch)
	if err != nil {
		sp.Finish(audit.Error, "preview branch", nil, err)
		return "", fmt.Errorf("reading stable front-door branch: %w", err)
	}
	if !frontDoorCommitPattern.MatchString(sha) {
		err := errors.New("stable front-door branch returned an invalid commit")
		sp.Finish(audit.Error, "preview branch", nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorApp()
	if err != nil {
		sp.Finish(audit.Error, "preview app", nil, err)
		return "", err
	}
	action := frontDoorActionCreate
	appID := ""
	currentFrontDomain := ""
	backendAppID := ""
	backendDomain := ""
	catalogPlan := managedFrontDoorCatalogPlan{Primary: normalized.ExpectedCatalogHash, Changed: true}
	if exists {
		var backend platformApplication
		action, backend, err = s.managedFrontDoorAction(app, normalized)
		if err != nil {
			sp.Finish(audit.Deny, "preview existing", nil, err)
			return "", err
		}
		if managedFrontDoorActionRequiresExternalCoordinator(action) && !s.managedFrontDoorExternalCoordinator {
			err = errors.New("managed front-door topology transition requires an external coordinator independent from the backend process")
			sp.Finish(audit.Deny, "preview existing", nil, err)
			return "", err
		}
		appID = app.UUID
		currentFrontDomain = app.domain()
		backendAppID = backend.UUID
		backendDomain = backend.domain()
		if action == frontDoorActionReconcile {
			entries, listErr := s.coolify.listEnvironmentVariables(context.Background(), app.UUID)
			if listErr != nil {
				sp.Finish(audit.Error, "preview catalog", nil, listErr)
				return "", listErr
			}
			catalogPlan, err = planManagedFrontDoorCatalogTransition(entries, s.coolify.token, normalized.ExpectedCatalogHash)
			if err != nil {
				sp.Finish(audit.Deny, "preview catalog", nil, err)
				return "", err
			}
		}
	}
	plan, err := s.plans.Create("platform-front-door-create", map[string]string{
		"action": action, "app": appID, "domain": normalized.Domain, "backend_url": normalized.BackendURL,
		"expected_protocol": normalized.ExpectedProtocol, "expected_catalog_hash": normalized.ExpectedCatalogHash,
		"branch_sha": sha, "server_uuid": s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"environment_uuid": s.coolify.environmentUUID, "environment_name": s.coolify.environmentName,
		"destination_uuid": s.coolify.destinationUUID, "github_app_uuid": s.coolify.githubAppUUID,
		"front_domain_before": currentFrontDomain, "backend_app": backendAppID, "backend_domain_before": backendDomain,
		"catalog_primary": catalogPlan.Primary, "catalog_transition": catalogPlan.Transition,
		"catalog_remove_uuid": catalogPlan.RemoveUUID, "catalog_changed": fmt.Sprintf("%t", catalogPlan.Changed),
	})
	if err != nil {
		sp.Finish(audit.Error, "preview plan", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("action: %s\napplication_name: %s\napplication_uuid: %s\nrepository: %s\nbranch: %s\nbranch_sha: %s\ndockerfile_location: %s\nport: %s\ndomain: %s\nbackend_origin: %s\nexpected_protocol: %s\nexpected_catalog_hash: %s\nauto_deploy: disabled\ninstant_deploy: disabled\nmounts: none\neffect: %s\nplan_id: %s\nexpiry: %s\n",
		action, managedFrontDoorName, appID, s.managedFrontDoorRepository(), managedFrontDoorBranch, sha,
		managedFrontDoorDockerfile, managedFrontDoorPort, normalized.Domain, normalized.BackendURL,
		normalized.ExpectedProtocol, normalized.ExpectedCatalogHash, managedFrontDoorEffect(action), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) PlatformFrontDoorCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_front_door_create")
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
		return "APPROVAL REQUIRED: platform_front_door_create would execute the reviewed managed front-door plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-front-door-create")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["server_uuid"] != s.coolify.serverUUID || plan.Args["project_uuid"] != s.coolify.projectUUID ||
		plan.Args["environment_uuid"] != s.coolify.environmentUUID || plan.Args["environment_name"] != s.coolify.environmentName ||
		plan.Args["destination_uuid"] != s.coolify.destinationUUID || plan.Args["github_app_uuid"] != s.coolify.githubAppUUID {
		err := errors.New("front-door platform configuration changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	sha, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorBranch)
	if err != nil || sha != plan.Args["branch_sha"] {
		err = errors.New("stable front-door branch changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	request, err := s.normalizeFrontDoorRequest(PlatformFrontDoorRequest{
		Domain: plan.Args["domain"], BackendURL: plan.Args["backend_url"], ExpectedProtocol: plan.Args["expected_protocol"],
		ExpectedCatalogHash: plan.Args["expected_catalog_hash"],
	})
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorApp()
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if plan.Args["action"] == "reconcile" && (!exists || app.UUID != plan.Args["app"]) {
		err := errors.New("managed front-door application changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	created := false
	catalogPlan := managedFrontDoorCatalogPlan{Primary: request.ExpectedCatalogHash, Changed: true}
	if exists {
		action, backend, actionErr := s.managedFrontDoorAction(app, request)
		if actionErr != nil {
			sp.Finish(audit.Deny, planID, nil, actionErr)
			return "", actionErr
		}
		if managedFrontDoorActionRequiresExternalCoordinator(action) && !s.managedFrontDoorExternalCoordinator {
			err := errors.New("managed front-door topology transition requires an external coordinator independent from the backend process")
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		if action != plan.Args["action"] || app.domain() != plan.Args["front_domain_before"] || backend.UUID != plan.Args["backend_app"] || backend.domain() != plan.Args["backend_domain_before"] {
			err := errors.New("managed front-door topology changed after preview")
			sp.Finish(audit.Deny, planID, nil, err)
			return "", err
		}
		if action == frontDoorActionReconcile {
			entries, listErr := s.coolify.listEnvironmentVariables(context.Background(), app.UUID)
			if listErr != nil {
				sp.Finish(audit.Error, planID, nil, listErr)
				return "", listErr
			}
			catalogPlan, err = planManagedFrontDoorCatalogTransition(entries, s.coolify.token, request.ExpectedCatalogHash)
			if err != nil {
				sp.Finish(audit.Deny, planID, nil, err)
				return "", err
			}
			if catalogPlan.Primary != plan.Args["catalog_primary"] || catalogPlan.Transition != plan.Args["catalog_transition"] ||
				catalogPlan.RemoveUUID != plan.Args["catalog_remove_uuid"] || fmt.Sprintf("%t", catalogPlan.Changed) != plan.Args["catalog_changed"] {
				err = errors.New("managed front-door catalog state changed after preview")
				sp.Finish(audit.Deny, planID, nil, err)
				return "", err
			}
		}
		if action == frontDoorActionRenameTemporary || action == frontDoorActionCutover ||
			action == frontDoorActionResumeCutoverBackend || action == frontDoorActionResumeCutoverPublic ||
			action == frontDoorActionRollback {
			out, transitionErr := s.executeManagedFrontDoorTransition(action, app, backend, request, sha)
			if transitionErr != nil {
				sp.Finish(audit.Error, planID, nil, transitionErr)
				return out, transitionErr
			}
			sp.Finish(audit.Allow, planID, nil, nil)
			return out, nil
		}
	} else {
		app, err = s.createManagedFrontDoorApp()
		if err != nil {
			sp.Finish(audit.Error, planID, nil, err)
			return "", err
		}
		created = true
	}
	if err := s.ensureManagedFrontDoorDomain(app, request.Domain); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return fmt.Sprintf("application_uuid: %s\napplication_created: %t\ndeployed: false\n", app.UUID, created), err
	}
	vars := map[string]string{
		"MCP_FRONT_DOOR_BACKEND_URL":       request.BackendURL,
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL": request.ExpectedProtocol,
		frontDoorExpectedCatalogKey:        catalogPlan.Primary,
	}
	if catalogPlan.Transition != "" {
		vars[frontDoorTransitionCatalogKey] = catalogPlan.Transition
	}
	if catalogPlan.RemoveUUID != "" {
		if err := s.coolify.deleteEnvironmentVariable(context.Background(), app.UUID, catalogPlan.RemoveUUID); err != nil {
			sp.Finish(audit.Error, planID, nil, err)
			return fmt.Sprintf("application_uuid: %s\napplication_created: %t\ndeployed: false\n", app.UUID, created), err
		}
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, err := s.coolify.setEnvironmentVariables(context.Background(), app.UUID, vars, keys); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return fmt.Sprintf("application_uuid: %s\napplication_created: %t\ndeployed: false\n", app.UUID, created), err
	}
	out := fmt.Sprintf("application_uuid: %s\napplication_created: %t\nbranch: %s\nexpected_commit: %s\nenvironment_variables_configured: %d\n", app.UUID, created, managedFrontDoorBranch, sha, len(vars))
	if app.commit() == sha && app.Status == "running:healthy" && !catalogPlan.Changed {
		sp.Finish(audit.Allow, planID, nil, nil)
		return out + "deployment_skipped: already_serving_expected_commit\n", nil
	}
	status, body, err := s.coolify.deploy(context.Background(), app.UUID, false)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return out + "deployed: unknown\n", fmt.Errorf("front-door deployment request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("front-door deployment -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return out, err
	}
	deployment := decodePlatformDeployResponse(body)
	sp.Finish(audit.Allow, planID, nil, nil)
	return out + fmt.Sprintf("deployed: true\ndeployment_id: %s\ndeployment_status: %s\n", deployment.DeploymentUUID, deployment.Status), nil
}

func (s *PlatformCapability) PlatformFrontDoorStatus() (string, error) {
	sp := s.log.Start("platform_front_door_status")
	if err := s.coolify.configError(); err != nil {
		sp.Finish(audit.Deny, "status", nil, err)
		return "", err
	}
	app, exists, err := s.managedFrontDoorApp()
	if err != nil {
		sp.Finish(audit.Error, "status", nil, err)
		return "", err
	}
	if !exists {
		sp.Finish(audit.Allow, "status absent", nil, nil)
		return "application: absent\n", nil
	}
	contract := "valid"
	if err := s.validateManagedFrontDoorApp(app, app.domain()); err != nil {
		contract = "invalid"
	}
	sp.Finish(audit.Allow, "status "+app.UUID, nil, nil)
	return s.redact(fmt.Sprintf("application_uuid: %s\nname: %s\nstatus: %s\ndeployment_state: %s\nrepository: %s\nbranch: %s\ncommit: %s\ndomain: %s\nbuild_pack: %s\ndockerfile_location: %s\nport: %s\nauto_deploy: %t\ninstant_deploy: %t\nhealthcheck_path: %s\ncontract: %s\n",
		app.UUID, app.Name, app.Status, app.DeploymentStatus, safePlatformURL(app.repo()), app.branch(), app.commit(),
		safePlatformURL(app.domain()), app.BuildPack, app.Dockerfile, app.PortsExposes, app.AutoDeploy, app.InstantDeploy,
		app.HealthcheckPath, contract)), nil
}

func managedFrontDoorActionRequiresExternalCoordinator(action string) bool {
	switch action {
	case frontDoorActionCutover, frontDoorActionResumeCutoverBackend, frontDoorActionResumeCutoverPublic, frontDoorActionRollback:
		return true
	default:
		return false
	}
}

func (s *PlatformCapability) frontDoorPlatformConfigError() error {
	if err := s.coolify.builderConfigError(); err != nil {
		return err
	}
	if len(s.coolify.allowedDomainRules) == 0 {
		return errors.New("COOLIFY_ALLOWED_DOMAINS is required for the managed front door")
	}
	if s.coolify.destinationUUID == "" {
		return errors.New("COOLIFY_DESTINATION_UUID is required for the managed front door")
	}
	return s.github.configError()
}

func (s *PlatformCapability) normalizeFrontDoorRequest(request PlatformFrontDoorRequest) (PlatformFrontDoorRequest, error) {
	domain, domainHost, err := s.normalizeFrontDoorOrigin(request.Domain, "front-door domain")
	if err != nil {
		return request, err
	}
	backend, backendHost, err := s.normalizeFrontDoorOrigin(request.BackendURL, "front-door backend")
	if err != nil {
		return request, err
	}
	if domainHost == backendHost {
		return request, errors.New("front-door domain and backend origin must be different")
	}
	request.ExpectedProtocol = strings.TrimSpace(request.ExpectedProtocol)
	request.ExpectedCatalogHash = strings.TrimSpace(request.ExpectedCatalogHash)
	if !frontDoorProtocolPattern.MatchString(request.ExpectedProtocol) || !frontDoorCatalogPattern.MatchString(request.ExpectedCatalogHash) {
		return request, errors.New("front-door compatibility identity is invalid")
	}
	request.Domain = domain
	request.BackendURL = backend
	return request, nil
}

func (s *PlatformCapability) normalizeFrontDoorOrigin(raw, field string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", "", fmt.Errorf("%s must be an HTTPS origin without credentials, explicit port, query, fragment or path", field)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	origin := parsed.String()
	if !s.coolify.domainAllowed(origin) {
		return "", "", fmt.Errorf("%s %q is not in COOLIFY_ALLOWED_DOMAINS", field, origin)
	}
	return origin, strings.ToLower(parsed.Hostname()), nil
}

func (s *PlatformCapability) managedFrontDoorRepository() string {
	return "https://github.com/" + s.github.owner + "/mcp-devbox.git"
}

func (s *PlatformCapability) managedFrontDoorApp() (platformApplication, bool, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		return platformApplication{}, false, fmt.Errorf("listing Coolify applications: %w", err)
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, false, fmt.Errorf("listing Coolify applications -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	apps, err := decodePlatformApplications(body)
	if err != nil {
		return platformApplication{}, false, err
	}
	var matches []platformApplication
	for _, app := range apps {
		if app.Name == managedFrontDoorName {
			matches = append(matches, app)
		}
	}
	if len(matches) == 0 {
		return platformApplication{}, false, nil
	}
	if len(matches) != 1 || !coolifyUUIDRe.MatchString(matches[0].UUID) {
		return platformApplication{}, false, errors.New("managed front-door application identity is ambiguous")
	}
	status, body, err = s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+url.PathEscape(matches[0].UUID), nil)
	if err != nil {
		return platformApplication{}, false, err
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, false, fmt.Errorf("reading managed front-door application -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		return platformApplication{}, false, err
	}
	if app.UUID == "" {
		app.UUID = matches[0].UUID
	}
	return app, true, nil
}

func (s *PlatformCapability) validateManagedFrontDoorApp(app platformApplication, domain string) error {
	if app.UUID == "" || app.Name != managedFrontDoorName || !s.managedFrontDoorRepositoryMatches(app.repo()) ||
		app.branch() != managedFrontDoorBranch || app.BuildPack != "dockerfile" ||
		app.Dockerfile != managedFrontDoorDockerfile || app.PortsExposes != managedFrontDoorPort || app.AutoDeploy ||
		app.InstantDeploy || app.HealthcheckPath != managedFrontDoorHealthPath {
		return errors.New("existing front-door application does not match the managed contract")
	}
	if app.domain() != "" && !s.coolify.domainAllowed(app.domain()) {
		return errors.New("existing front-door domain is outside COOLIFY_ALLOWED_DOMAINS")
	}
	return nil
}

func (s *PlatformCapability) managedFrontDoorRepositoryMatches(raw string) bool {
	normalized := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(raw), "/"), ".git")
	ownerRepo := s.github.owner + "/mcp-devbox"
	return strings.EqualFold(normalized, ownerRepo) || strings.EqualFold(normalized, "https://github.com/"+ownerRepo)
}

func (s *PlatformCapability) ensureManagedFrontDoorDomain(app platformApplication, domain string) error {
	if app.domain() == domain {
		return nil
	}
	if app.domain() != "" {
		return errors.New("existing front-door application domain does not match the managed contract")
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(app.UUID), map[string]any{"domains": domain})
	if err != nil {
		return fmt.Errorf("configuring managed front-door domain: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("configuring managed front-door domain -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	return nil
}

func (s *PlatformCapability) createManagedFrontDoorApp() (platformApplication, error) {
	payload := map[string]any{
		"name": managedFrontDoorName, "server_uuid": s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"destination_uuid": s.coolify.destinationUUID, "git_repository": s.managedFrontDoorRepository(),
		"git_branch": managedFrontDoorBranch, "build_pack": "dockerfile", "dockerfile_location": managedFrontDoorDockerfile,
		"ports_exposes": managedFrontDoorPort, "ports_mappings": "", "autogenerate_domain": false,
		"is_auto_deploy_enabled": false, "instant_deploy": false, "custom_docker_run_options": "",
		"health_check_enabled": true, "health_check_type": "http", "health_check_scheme": "http",
		"health_check_method": "GET", "health_check_path": managedFrontDoorHealthPath, "health_check_port": 8765,
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
		return platformApplication{}, fmt.Errorf("creating managed front-door application -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil || !coolifyUUIDRe.MatchString(app.UUID) {
		return platformApplication{}, errors.New("created front-door application identity is invalid")
	}
	return app, nil
}
