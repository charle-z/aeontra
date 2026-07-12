package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// coolifyUUIDRe restricts the app identifier the agent may pass to a safe, opaque
// token — it can never contain a scheme, slash, or query, so it cannot rewrite the
// request target (no SSRF via the uuid).
var coolifyUUIDRe = regexp.MustCompile(`^[a-zA-Z0-9]{1,64}$`)

// CoolifyClient triggers deploys against a single, operator-configured Coolify
// instance. The API token is a secret: it is sent ONLY in the Authorization header,
// never placed in the URL, returned, or logged. The base URL is fixed by config, so
// the agent cannot point this at an arbitrary host.
type CoolifyClient struct {
	baseURL            string
	token              string
	allowed            map[string]bool // optional allowlist of app uuids; empty = allow any
	serverUUID         string
	projectUUID        string
	environmentName    string
	environmentUUID    string
	githubAppUUID      string
	destinationUUID    string
	allowedDomainRules []string
	allowedMounts      map[string]bool
	do                 func(*http.Request) (*http.Response, error)
}

// NewCoolifyClient builds a client. A zero client (nil) means "not configured".
func NewCoolifyClient(baseURL, token string, allowedApps []string) *CoolifyClient {
	allowed := map[string]bool{}
	for _, a := range allowedApps {
		if a = strings.TrimSpace(a); a != "" {
			allowed[a] = true
		}
	}
	return &CoolifyClient{
		baseURL:       strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:         strings.TrimSpace(token),
		allowed:       allowed,
		allowedMounts: map[string]bool{},
		do:            (&http.Client{Timeout: 30 * time.Second}).Do,
	}
}

// Configured reports whether a base URL and token are set.
func (c *CoolifyClient) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

// WithBuilderConfig adds the optional Coolify app-creation context.
func (c *CoolifyClient) WithBuilderConfig(serverUUID, projectUUID, environmentName, environmentUUID string, allowedDomains []string) *CoolifyClient {
	c.serverUUID = strings.TrimSpace(serverUUID)
	c.projectUUID = strings.TrimSpace(projectUUID)
	c.environmentName = strings.TrimSpace(environmentName)
	c.environmentUUID = strings.TrimSpace(environmentUUID)
	c.allowedDomainRules = nil
	for _, d := range allowedDomains {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			c.allowedDomainRules = append(c.allowedDomainRules, d)
		}
	}
	return c
}

// WithGitHubApp configures the Coolify GitHub App source used for repository
// creation. When empty, builder creation retains the public-repository API for
// backwards compatibility.
func (c *CoolifyClient) WithGitHubApp(uuid string) *CoolifyClient {
	c.githubAppUUID = strings.TrimSpace(uuid)
	return c
}

// WithBuilderRuntime fixes the deployment destination and the exact mount specs an
// agent may request. Repository content and model output cannot enlarge this
// administrator-owned allowlist.
func (c *CoolifyClient) WithBuilderRuntime(destinationUUID string, allowedMounts []string) *CoolifyClient {
	c.destinationUUID = strings.TrimSpace(destinationUUID)
	c.allowedMounts = map[string]bool{}
	for _, mount := range allowedMounts {
		if mount = strings.TrimSpace(mount); mount != "" {
			c.allowedMounts[mount] = true
		}
	}
	return c
}

func (c *CoolifyClient) mountAllowed(mount string) bool {
	return c != nil && c.allowedMounts[strings.TrimSpace(mount)]
}

func (c *CoolifyClient) appAllowed(uuid string) bool {
	if len(c.allowed) == 0 {
		return true // no allowlist configured -> any valid uuid (still mode-gated)
	}
	return c.allowed[uuid]
}

func (c *CoolifyClient) builderConfigured() bool {
	return c.Configured() && c.serverUUID != "" && c.projectUUID != "" &&
		(c.environmentName != "" || c.environmentUUID != "")
}

// deploy calls Coolify's deploy-by-uuid endpoint. Returns status code + (truncated)
// body. The token rides only in the header.
func (c *CoolifyClient) deploy(ctx context.Context, uuid string) (int, string, error) {
	u := c.baseURL + "/api/v1/deploy?" + url.Values{"uuid": {uuid}, "force": {"false"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}

func (c *CoolifyClient) request(ctx context.Context, method, path string, payload any) (int, string, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, "", err
		}
		body = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, strings.TrimSpace(string(data)), nil
}

// CoolifyDeploy triggers a deploy of the given app (uuid) on the configured Coolify
// instance. It is disabled unless configured, mode-gated (read-only denies; ask needs
// approve=true), optionally allowlisted, audited, and its response is redacted. The
// API token is never exposed to the agent.
func (s *Service) CoolifyDeploy(app string, approve bool) (string, error) {
	sp := s.log.Start("coolify_deploy")
	if s.coolify == nil || !s.coolify.Configured() {
		err := fmt.Errorf("coolify_deploy is not configured (set COOLIFY_URL and COOLIFY_API_TOKEN)")
		sp.Finish(audit.Deny, "coolify_deploy", nil, err)
		return "", err
	}
	app = strings.TrimSpace(app)
	if !coolifyUUIDRe.MatchString(app) {
		err := fmt.Errorf("invalid app id (expected an alphanumeric Coolify uuid)")
		sp.Finish(audit.Deny, "coolify_deploy "+summarize(app), nil, err)
		return "", err
	}
	if !s.coolify.appAllowed(app) {
		err := fmt.Errorf("app %q is not in COOLIFY_ALLOWED_APPS", app)
		sp.Finish(audit.Deny, "coolify_deploy "+app, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, "coolify_deploy "+app, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "coolify_deploy "+app, nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: coolify_deploy would trigger a deploy of app %s. Re-invoke with approve=true.", app), nil
	}
	status, body, err := s.coolify.deploy(context.Background(), app)
	if err != nil {
		sp.Finish(audit.Error, "coolify_deploy "+app, nil, err)
		return "", fmt.Errorf("coolify deploy request failed: %w", err)
	}
	sp.Finish(audit.Allow, "coolify_deploy "+app, nil, nil)
	return fmt.Sprintf("coolify deploy %s -> HTTP %d\n%s", app, status, s.redact(body)), nil
}

func (s *Service) CoolifyListApps() (string, error) {
	sp := s.log.Start("coolify_list_apps")
	if s.coolify == nil || !s.coolify.Configured() {
		err := fmt.Errorf("coolify_list_apps is not configured (set COOLIFY_URL and COOLIFY_API_TOKEN)")
		sp.Finish(audit.Deny, "coolify_list_apps", nil, err)
		return "", err
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications", nil)
	if err != nil {
		sp.Finish(audit.Error, "coolify_list_apps", nil, err)
		return "", fmt.Errorf("coolify list apps request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		sp.Finish(audit.Error, "coolify_list_apps", nil, fmt.Errorf("HTTP %d", status))
		return s.redact(body), fmt.Errorf("coolify list apps -> HTTP %d: %s", status, s.redact(body))
	}
	sp.Finish(audit.Allow, "coolify_list_apps", nil, nil)
	return s.redact(body), nil
}

func (s *Service) CoolifyAppStatus(app string) (string, error) {
	sp := s.log.Start("coolify_app_status")
	if s.coolify == nil || !s.coolify.Configured() {
		err := fmt.Errorf("coolify_app_status is not configured (set COOLIFY_URL and COOLIFY_API_TOKEN)")
		sp.Finish(audit.Deny, "coolify_app_status", nil, err)
		return "", err
	}
	app = strings.TrimSpace(app)
	if err := s.checkCoolifyApp("coolify_app_status", app, sp); err != nil {
		return "", err
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+url.PathEscape(app), nil)
	if err != nil {
		sp.Finish(audit.Error, "coolify_app_status "+app, nil, err)
		return "", fmt.Errorf("coolify app status request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		sp.Finish(audit.Error, "coolify_app_status "+app, nil, fmt.Errorf("HTTP %d", status))
		return s.redact(body), fmt.Errorf("coolify app status -> HTTP %d: %s", status, s.redact(body))
	}
	sp.Finish(audit.Allow, "coolify_app_status "+app, nil, nil)
	return s.redact(body), nil
}

func (s *Service) CoolifyCreateApp(name, githubRepo, branch, buildPack, port, domain string, approve bool) (string, error) {
	sp := s.log.Start("coolify_create_app")
	if s.coolify == nil || !s.coolify.Configured() {
		err := fmt.Errorf("coolify_create_app is not configured (set COOLIFY_URL and COOLIFY_API_TOKEN)")
		sp.Finish(audit.Deny, "coolify_create_app", nil, err)
		return "", err
	}
	if !s.coolify.builderConfigured() {
		err := fmt.Errorf("coolify_create_app requires COOLIFY_SERVER_UUID, COOLIFY_PROJECT_UUID, and COOLIFY_ENVIRONMENT_NAME or COOLIFY_ENVIRONMENT_UUID")
		sp.Finish(audit.Deny, "coolify_create_app", nil, err)
		return "", err
	}
	name = strings.TrimSpace(name)
	if !safeCloneDir(name) {
		err := fmt.Errorf("invalid Coolify app name %q", name)
		sp.Finish(audit.Deny, "coolify_create_app", nil, err)
		return "", err
	}
	githubRepo = strings.TrimSpace(githubRepo)
	if err := validateGitRepoRef(githubRepo); err != nil {
		sp.Finish(audit.Deny, "coolify_create_app "+name, nil, err)
		return "", err
	}
	branch = defaultGitName(branch, "main")
	if !safeGitName(branch) {
		err := fmt.Errorf("invalid git branch %q", branch)
		sp.Finish(audit.Deny, "coolify_create_app "+name, nil, err)
		return "", err
	}
	buildPack = strings.ToLower(strings.TrimSpace(buildPack))
	if buildPack == "" {
		buildPack = "nixpacks"
	}
	if !validCoolifyBuildPack(buildPack) {
		err := fmt.Errorf("invalid build_pack %q", buildPack)
		sp.Finish(audit.Deny, "coolify_create_app "+name, nil, err)
		return "", err
	}
	if strings.TrimSpace(domain) != "" && !s.coolify.domainAllowed(domain) {
		err := fmt.Errorf("domain %q is not in COOLIFY_ALLOWED_DOMAINS", domain)
		sp.Finish(audit.Deny, "coolify_create_app "+name, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, "coolify_create_app "+name, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "coolify_create_app "+name, nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: coolify_create_app would create app %s from %s. Re-invoke with approve=true.", name, githubRepo), nil
	}
	payload := map[string]any{
		"name":           name,
		"server_uuid":    s.coolify.serverUUID,
		"project_uuid":   s.coolify.projectUUID,
		"git_repository": githubRepo,
		"git_branch":     branch,
		"build_pack":     buildPack,
	}
	if s.coolify.environmentUUID != "" {
		payload["environment_uuid"] = s.coolify.environmentUUID
	} else {
		payload["environment_name"] = s.coolify.environmentName
	}
	if strings.TrimSpace(port) != "" {
		payload["ports_exposes"] = strings.TrimSpace(port)
	}
	if strings.TrimSpace(domain) != "" {
		payload["fqdn"] = strings.TrimSpace(domain)
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/public", payload)
	if err != nil {
		sp.Finish(audit.Error, "coolify_create_app "+name, nil, err)
		return "", fmt.Errorf("coolify create app request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		sp.Finish(audit.Error, "coolify_create_app "+name, nil, fmt.Errorf("HTTP %d", status))
		return s.redact(body), fmt.Errorf("coolify create app -> HTTP %d: %s", status, s.redact(body))
	}
	sp.Finish(audit.Allow, "coolify_create_app "+name, nil, nil)
	return fmt.Sprintf("coolify create app -> HTTP %d\n%s", status, s.redact(body)), nil
}

func (s *Service) CoolifySetEnv(app string, vars map[string]string, approve bool) (string, error) {
	sp := s.log.Start("coolify_set_env")
	if s.coolify == nil || !s.coolify.Configured() {
		err := fmt.Errorf("coolify_set_env is not configured (set COOLIFY_URL and COOLIFY_API_TOKEN)")
		sp.Finish(audit.Deny, "coolify_set_env", nil, err)
		return "", err
	}
	app = strings.TrimSpace(app)
	if err := s.checkCoolifyApp("coolify_set_env", app, sp); err != nil {
		return "", err
	}
	if len(vars) == 0 {
		err := fmt.Errorf("at least one env var is required")
		sp.Finish(audit.Error, "coolify_set_env "+app, nil, err)
		return "", err
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if !validEnvKey(k) {
			err := fmt.Errorf("invalid env key %q", k)
			sp.Finish(audit.Deny, "coolify_set_env "+app, nil, err)
			return "", err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, "coolify_set_env "+app, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "coolify_set_env "+app+" "+strings.Join(keys, ","), nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: coolify_set_env would set %d variable(s) on %s. Re-invoke with approve=true.", len(keys), app), nil
	}
	var summaries []string
	for _, k := range keys {
		payload := map[string]any{"key": k, "value": vars[k]}
		status, body, err := s.coolify.request(context.Background(), http.MethodPost, "/api/v1/applications/"+url.PathEscape(app)+"/envs", payload)
		if err != nil {
			sp.Finish(audit.Error, "coolify_set_env "+app+" "+k, nil, err)
			return "", fmt.Errorf("coolify set env request failed: %w", err)
		}
		if status < 200 || status >= 300 {
			sp.Finish(audit.Error, "coolify_set_env "+app+" "+k, nil, fmt.Errorf("HTTP %d", status))
			return s.redact(body), fmt.Errorf("coolify set env -> HTTP %d: %s", status, s.redact(body))
		}
		summaries = append(summaries, fmt.Sprintf("%s -> HTTP %d: %s", k, status, s.redact(body)))
	}
	sp.Finish(audit.Allow, "coolify_set_env "+app+" "+strings.Join(keys, ","), nil, nil)
	return "coolify env updated:\n" + strings.Join(summaries, "\n"), nil
}

func (s *Service) checkCoolifyApp(tool, app string, sp *audit.Span) error {
	if !coolifyUUIDRe.MatchString(app) {
		err := fmt.Errorf("invalid app id (expected an alphanumeric Coolify uuid)")
		sp.Finish(audit.Deny, tool+" "+summarize(app), nil, err)
		return err
	}
	if !s.coolify.appAllowed(app) {
		err := fmt.Errorf("app %q is not in COOLIFY_ALLOWED_APPS", app)
		sp.Finish(audit.Deny, tool+" "+app, nil, err)
		return err
	}
	return nil
}

func validateGitRepoRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("github_repo is required")
	}
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "git@") {
		return validateCloneURL(ref)
	}
	parts := strings.Split(ref, "/")
	if len(parts) != 2 || !safeCloneDir(parts[0]) || !safeCloneDir(parts[1]) {
		return fmt.Errorf("github_repo must be owner/repo or a credential-free Git URL")
	}
	return nil
}

func validCoolifyBuildPack(v string) bool {
	switch v {
	case "nixpacks", "dockerfile", "static", "dockercompose":
		return true
	default:
		return false
	}
}

func (c *CoolifyClient) domainAllowed(raw string) bool {
	if len(c.allowedDomainRules) == 0 {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(raw))
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = strings.ToLower(u.Hostname())
	}
	for _, rule := range c.allowedDomainRules {
		if host == rule || strings.HasSuffix(host, "."+rule) {
			return true
		}
	}
	return false
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validEnvKey(k string) bool {
	return envKeyRe.MatchString(strings.TrimSpace(k))
}
