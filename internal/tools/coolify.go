package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	baseURL string
	token   string
	allowed map[string]bool // optional allowlist of app uuids; empty = allow any
	do      func(*http.Request) (*http.Response, error)
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
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		allowed: allowed,
		do:      (&http.Client{Timeout: 30 * time.Second}).Do,
	}
}

// Configured reports whether a base URL and token are set.
func (c *CoolifyClient) Configured() bool {
	return c != nil && c.baseURL != "" && c.token != ""
}

func (c *CoolifyClient) appAllowed(uuid string) bool {
	if len(c.allowed) == 0 {
		return true // no allowlist configured -> any valid uuid (still mode-gated)
	}
	return c.allowed[uuid]
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
