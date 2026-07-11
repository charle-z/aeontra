package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

type PlatformAppCreateRequest struct {
	Name                string
	GitHubRepo          string
	Branch              string
	Domain              string
	Port                string
	BuildPack           string
	HealthcheckPath     string
	HealthcheckInterval int
	HealthcheckTimeout  int
	RequiredEnv         []string
}

type platformApplication struct {
	UUID             string `json:"uuid"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	DeploymentStatus string `json:"deployment_status"`
	Repository       string `json:"repository"`
	GitRepository    string `json:"git_repository"`
	Branch           string `json:"branch"`
	GitBranch        string `json:"git_branch"`
	FQDN             string `json:"fqdn"`
	Domain           string `json:"domain"`
	GitCommitSHA     string `json:"git_commit_sha"`
	SourceCommit     string `json:"source_commit"`
}

func (a platformApplication) repo() string {
	if a.GitRepository != "" {
		return a.GitRepository
	}
	return a.Repository
}

func (a platformApplication) branch() string {
	if a.GitBranch != "" {
		return a.GitBranch
	}
	return a.Branch
}

func (a platformApplication) domain() string {
	if a.FQDN != "" {
		return a.FQDN
	}
	return a.Domain
}

func (a platformApplication) commit() string {
	if a.GitCommitSHA != "" {
		return a.GitCommitSHA
	}
	return a.SourceCommit
}

func (c *CoolifyClient) configError() error {
	var missing []string
	if c == nil || strings.TrimSpace(c.baseURL) == "" {
		missing = append(missing, "COOLIFY_URL")
	}
	if c == nil || strings.TrimSpace(c.token) == "" {
		missing = append(missing, "COOLIFY_API_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("Coolify configuration missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *CoolifyClient) builderConfigError() error {
	if err := c.configError(); err != nil {
		return err
	}
	var missing []string
	if c.serverUUID == "" {
		missing = append(missing, "COOLIFY_SERVER_UUID")
	}
	if c.projectUUID == "" {
		missing = append(missing, "COOLIFY_PROJECT_UUID")
	}
	if c.environmentUUID == "" && c.environmentName == "" {
		missing = append(missing, "COOLIFY_ENVIRONMENT_UUID or COOLIFY_ENVIRONMENT_NAME")
	}
	if len(missing) > 0 {
		return fmt.Errorf("Coolify builder configuration missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Service) PlatformAppsList() (string, error) {
	sp := s.log.Start("platform_apps_list")
	if err := s.coolify.configError(); err != nil {
		sp.Finish(audit.Deny, "list", nil, err)
		return "", err
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		sp.Finish(audit.Error, "list", nil, err)
		return "", fmt.Errorf("Coolify application list request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("Coolify application list -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, "list", nil, err)
		return s.coolifySafe(body), err
	}
	var apps []platformApplication
	if err := json.Unmarshal([]byte(body), &apps); err != nil {
		sp.Finish(audit.Error, "list", nil, err)
		return "", fmt.Errorf("decoding Coolify application list: %w", err)
	}
	var b strings.Builder
	for i, app := range apps {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(formatPlatformApp(app))
	}
	if len(apps) == 0 {
		b.WriteString("applications: []\n")
	}
	sp.Finish(audit.Allow, "list", nil, nil)
	return s.redact(b.String()), nil
}

func (s *Service) PlatformAppStatus(appID string) (string, error) {
	sp := s.log.Start("platform_app_status")
	app, err := s.getPlatformApp(appID)
	if err != nil {
		sp.Finish(audit.Deny, "status "+summarize(appID), nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "status "+app.UUID, nil, nil)
	return s.redact(formatPlatformApp(app)), nil
}

func (s *Service) PlatformAppCreatePreview(req PlatformAppCreateRequest) (string, error) {
	sp := s.log.Start("platform_app_create_preview")
	if err := s.coolify.builderConfigError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	normalized, err := s.normalizePlatformCreate(req)
	if err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	envNames := append([]string(nil), normalized.RequiredEnv...)
	sort.Strings(envNames)
	args := map[string]string{
		"name": normalized.Name, "repository": normalized.GitHubRepo, "branch": normalized.Branch,
		"domain": normalized.Domain, "port": normalized.Port, "build_pack": normalized.BuildPack,
		"healthcheck_path":     normalized.HealthcheckPath,
		"healthcheck_interval": strconv.Itoa(normalized.HealthcheckInterval),
		"healthcheck_timeout":  strconv.Itoa(normalized.HealthcheckTimeout),
		"required_env":         strings.Join(envNames, ","),
		"server_uuid":          s.coolify.serverUUID, "project_uuid": s.coolify.projectUUID,
		"environment_uuid": s.coolify.environmentUUID, "environment_name": s.coolify.environmentName,
	}
	plan, err := s.plans.Create("platform-app-create", args)
	if err != nil {
		sp.Finish(audit.Error, "preview", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("server_uuid: %s\nproject_uuid: %s\nenvironment_uuid: %s\nenvironment_name: %s\nrepository: %s\nbranch: %s\nbuild_strategy: %s\ndomain: %s\nport: %s\nhealthcheck_path: %s\nhealthcheck_interval: %d\nhealthcheck_timeout: %d\nrequired_environment_variables: %s\neffect: POST one application to the configured Coolify project/environment\nplan_id: %s\nexpiry: %s\n",
		s.coolify.serverUUID, s.coolify.projectUUID, s.coolify.environmentUUID, s.coolify.environmentName,
		normalized.GitHubRepo, normalized.Branch, normalized.BuildPack, normalized.Domain, normalized.Port,
		normalized.HealthcheckPath, normalized.HealthcheckInterval, normalized.HealthcheckTimeout,
		strings.Join(envNames, ","), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) PlatformAppCreate(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_app_create")
	if err := s.coolify.builderConfigError(); err != nil {
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
		return "APPROVAL REQUIRED: platform_app_create would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-app-create")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if plan.Args["server_uuid"] != s.coolify.serverUUID || plan.Args["project_uuid"] != s.coolify.projectUUID ||
		plan.Args["environment_uuid"] != s.coolify.environmentUUID || plan.Args["environment_name"] != s.coolify.environmentName {
		err := fmt.Errorf("Coolify builder configuration changed after preview")
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	payload := map[string]any{
		"name": plan.Args["name"], "server_uuid": plan.Args["server_uuid"], "project_uuid": plan.Args["project_uuid"],
		"git_repository": plan.Args["repository"], "git_branch": plan.Args["branch"], "build_pack": plan.Args["build_pack"],
	}
	if plan.Args["environment_uuid"] != "" {
		payload["environment_uuid"] = plan.Args["environment_uuid"]
	} else {
		payload["environment_name"] = plan.Args["environment_name"]
	}
	if plan.Args["domain"] != "" {
		payload["fqdn"] = plan.Args["domain"]
	}
	if plan.Args["port"] != "" {
		payload["ports_exposes"] = plan.Args["port"]
	}
	if plan.Args["healthcheck_path"] != "" {
		payload["health_check_path"] = plan.Args["healthcheck_path"]
		payload["health_check_interval"] = parseCount(plan.Args["healthcheck_interval"])
		payload["health_check_timeout"] = parseCount(plan.Args["healthcheck_timeout"])
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/public", payload)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("Coolify create application request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("Coolify create application -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return s.coolifySafe(body), err
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("decoding created Coolify application: %w", err)
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return s.redact(formatPlatformApp(app)), nil
}

func (s *Service) PlatformDeployPreview(appID string) (string, error) {
	sp := s.log.Start("platform_deploy_preview")
	app, err := s.getPlatformApp(appID)
	if err != nil {
		sp.Finish(audit.Deny, "preview "+summarize(appID), nil, err)
		return "", err
	}
	plan, err := s.plans.Create("platform-deploy", map[string]string{
		"app": app.UUID, "name": app.Name, "repository": app.repo(), "branch": app.branch(), "commit": app.commit(),
	})
	if err != nil {
		sp.Finish(audit.Error, "preview "+app.UUID, nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, nil, nil)
	return fmt.Sprintf("app: %s\nname: %s\nrepository: %s\nbranch: %s\nexpected_commit: %s\neffect: trigger one non-force deployment for the configured Coolify application\nplan_id: %s\nexpiry: %s\n",
		app.UUID, app.Name, app.repo(), app.branch(), app.commit(), plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) PlatformDeploy(planID string, approve bool) (string, error) {
	sp := s.log.Start("platform_deploy")
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
		return "APPROVAL REQUIRED: platform_deploy would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "platform-deploy")
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
	status, body, err := s.coolify.deploy(context.Background(), app.UUID)
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", fmt.Errorf("Coolify deployment request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		err := fmt.Errorf("Coolify deployment -> HTTP %d: %s", status, s.coolifySafe(body))
		sp.Finish(audit.Error, planID, nil, err)
		return s.coolifySafe(body), err
	}
	var result struct {
		DeploymentUUID string `json:"deployment_uuid"`
		UUID           string `json:"uuid"`
		Status         string `json:"status"`
	}
	_ = json.Unmarshal([]byte(body), &result)
	if result.DeploymentUUID == "" {
		result.DeploymentUUID = result.UUID
	}
	sp.Finish(audit.Allow, planID, nil, nil)
	return fmt.Sprintf("deployment_id: %s\nstatus: %s\n", result.DeploymentUUID, result.Status), nil
}

func (s *Service) getPlatformApp(appID string) (platformApplication, error) {
	if err := s.coolify.configError(); err != nil {
		return platformApplication{}, err
	}
	appID = strings.TrimSpace(appID)
	if !coolifyUUIDRe.MatchString(appID) {
		return platformApplication{}, fmt.Errorf("invalid Coolify application id")
	}
	if !s.coolify.appAllowed(appID) {
		return platformApplication{}, fmt.Errorf("app %q is not in COOLIFY_ALLOWED_APPS", appID)
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID), nil)
	if err != nil {
		return platformApplication{}, fmt.Errorf("Coolify application request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, fmt.Errorf("Coolify application -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		return platformApplication{}, fmt.Errorf("decoding Coolify application: %w", err)
	}
	if app.UUID == "" {
		app.UUID = appID
	}
	return app, nil
}

func (s *Service) normalizePlatformCreate(req PlatformAppCreateRequest) (PlatformAppCreateRequest, error) {
	req.Name = strings.TrimSpace(req.Name)
	if !safeCloneDir(req.Name) {
		return req, fmt.Errorf("invalid Coolify application name %q", req.Name)
	}
	repo := strings.TrimSpace(req.GitHubRepo)
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") {
		clean, err := sanitizeGitHubRemoteURL(repo, s.github.owner)
		if err != nil {
			return req, err
		}
		repo = clean
	} else {
		parts := strings.Split(repo, "/")
		if len(parts) != 2 || !strings.EqualFold(parts[0], s.github.owner) || !safeCloneDir(parts[1]) {
			return req, fmt.Errorf("repository must be under configured GITHUB_OWNER %q", s.github.owner)
		}
		// Coolify's public-application API requires a clone URL. Keep the MCP
		// input ergonomic (owner/repo) while storing the exact HTTPS URL in the
		// reviewed plan.
		repo = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
	}
	req.GitHubRepo = repo
	req.Branch = defaultGitName(req.Branch, "main")
	if !safeGitName(req.Branch) {
		return req, fmt.Errorf("invalid git branch %q", req.Branch)
	}
	req.BuildPack = strings.ToLower(strings.TrimSpace(req.BuildPack))
	if req.BuildPack == "" {
		req.BuildPack = "nixpacks"
	}
	if !validCoolifyBuildPack(req.BuildPack) {
		return req, fmt.Errorf("invalid build pack %q", req.BuildPack)
	}
	// Coolify's current API requires ports_exposes even for its static build
	// pack. Static sites are served by the generated web server on port 80.
	if req.BuildPack == "static" && strings.TrimSpace(req.Port) == "" {
		req.Port = "80"
	}
	req.Domain = strings.TrimSpace(req.Domain)
	if req.Domain != "" && !s.coolify.domainAllowed(req.Domain) {
		return req, fmt.Errorf("domain %q is not in COOLIFY_ALLOWED_DOMAINS", req.Domain)
	}
	req.Port = strings.TrimSpace(req.Port)
	if req.Port != "" {
		port, err := strconv.Atoi(req.Port)
		if err != nil || port < 1 || port > 65535 {
			return req, fmt.Errorf("port must be an integer from 1 to 65535")
		}
	}
	req.HealthcheckPath = strings.TrimSpace(req.HealthcheckPath)
	if req.HealthcheckPath != "" && (!strings.HasPrefix(req.HealthcheckPath, "/") || strings.Contains(req.HealthcheckPath, "..") || strings.ContainsAny(req.HealthcheckPath, "\r\n")) {
		return req, fmt.Errorf("invalid healthcheck path")
	}
	if req.HealthcheckInterval < 0 || req.HealthcheckTimeout < 0 {
		return req, fmt.Errorf("healthcheck timing must be non-negative")
	}
	if req.HealthcheckPath != "" && req.HealthcheckInterval == 0 {
		req.HealthcheckInterval = 30
	}
	if req.HealthcheckPath != "" && req.HealthcheckTimeout == 0 {
		req.HealthcheckTimeout = 5
	}
	seen := map[string]bool{}
	var env []string
	for _, key := range req.RequiredEnv {
		key = strings.TrimSpace(key)
		if !validEnvKey(key) {
			return req, fmt.Errorf("invalid required environment variable name %q", key)
		}
		if !seen[key] {
			seen[key] = true
			env = append(env, key)
		}
	}
	req.RequiredEnv = env
	return req, nil
}

func formatPlatformApp(app platformApplication) string {
	return fmt.Sprintf("uuid: %s\nname: %s\nstatus: %s\ndeployment_state: %s\nrepository: %s\nbranch: %s\ndomain: %s\n",
		app.UUID, app.Name, app.Status, app.DeploymentStatus, safePlatformURL(app.repo()), app.branch(), safePlatformURL(app.domain()))
}

func safePlatformURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || (!strings.Contains(raw, "://") && !strings.HasPrefix(raw, "git@")) {
		return raw
	}
	if strings.HasPrefix(raw, "git@") {
		if err := validateCloneURL(raw); err == nil {
			return raw
		}
		return "[unsafe URL omitted]"
	}
	clean, err := sanitizeCredentialFreeURL(raw)
	if err != nil {
		return "[unsafe URL omitted]"
	}
	return clean
}

func (s *Service) coolifySafe(body string) string {
	if s.coolify != nil && s.coolify.token != "" {
		body = strings.ReplaceAll(body, s.coolify.token, "[REDACTED]")
	}
	return s.redact(body)
}
