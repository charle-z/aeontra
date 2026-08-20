package frontdoorcoordinator

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const statusDescriptionPrefix = "mcp-front-door-coordinator:v1 "

var (
	commitPattern   = regexp.MustCompile("^[a-f0-9]{40}$")
	protocolPattern = regexp.MustCompile("^[0-9]{4}-[0-9]{2}-[0-9]{2}$")
	catalogPattern  = regexp.MustCompile("^sha256:[a-f0-9]{64}$")

	ErrTopologyFrontApplication   = errors.New("front application topology read failed")
	ErrTopologyBackendApplication = errors.New("backend application topology read failed")
	ErrTopologyManagedIdentity    = errors.New("managed topology identity failed")
	ErrTopologyFrontBackend       = errors.New("front backend topology read failed")

	ErrCoolifyRequestBuild     = errors.New("coolify request build failed")
	ErrCoolifyRequestTransport = errors.New("coolify request transport failed")
	ErrCoolifyPrivateTarget    = errors.New("coolify private gateway target failed")
	ErrCoolifyPrivateResolve   = errors.New("coolify private gateway resolution failed")
	ErrCoolifyPrivateAddress   = errors.New("coolify private gateway address policy failed")
	ErrCoolifyPrivateRefused   = errors.New("coolify private gateway connection refused")
	ErrCoolifyPrivateTimeout   = errors.New("coolify private gateway connection timed out")
	ErrCoolifyPrivateRoute     = errors.New("coolify private gateway route unavailable")
	ErrCoolifyPrivateConnect   = errors.New("coolify private gateway connection failed")
	ErrCoolifyResponseRead     = errors.New("coolify response read failed")
	ErrCoolifyResponseHTTP     = errors.New("coolify response HTTP failed")
	ErrCoolifyResponseDecode   = errors.New("coolify response decode failed")
	ErrCoolifyIdentity         = errors.New("coolify application identity failed")
)

type Config struct {
	CoolifyURL            string
	CoolifyToken          string
	CoordinatorAppID      string
	FrontAppID            string
	BackendAppID          string
	ExpectedFrontCommit   string
	ExpectedBackendCommit string
	ExpectedProtocol      string
	ExpectedCatalogHash   string
}

type Client struct {
	config    Config
	http      *http.Client
	probeHTTP *http.Client
	sleep     func(time.Duration)
}

const (
	privateCoolifyHost   = "host.docker.internal"
	privateCoolifyScheme = "http+host-gateway"
)

func validateCoolifyOrigin(raw string) (*url.URL, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, false, errors.New("coolify URL must be a fixed origin")
	}
	switch parsed.Scheme {
	case "https":
		return parsed, false, nil
	case privateCoolifyScheme:
		if parsed.Port() == "" {
			return nil, false, errors.New("private Coolify gateway URL requires an explicit port")
		}
		return parsed, true, nil
	default:
		return nil, false, errors.New("coolify URL must use HTTPS or the managed private gateway scheme")
	}
}

func privateCoolifyAddress(address net.IP) bool {
	return address != nil && (address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast())
}

func privateCoolifyAddressesAllowed(addresses []net.IPAddr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !privateCoolifyAddress(address.IP) {
			return false
		}
	}
	return true
}

func privateCoolifyDialContext(expectedAddress string) func(context.Context, string, string) (net.Conn, error) {
	expectedHost, expectedPort, err := net.SplitHostPort(expectedAddress)
	if err != nil || expectedHost == "" || expectedPort == "" {
		return func(context.Context, string, string) (net.Conn, error) {
			return nil, ErrCoolifyPrivateTarget
		}
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, expectedHost) || port != expectedPort {
			return nil, ErrCoolifyPrivateTarget
		}
		addresses, err := privateCoolifyAddresses(ctx)
		if err != nil {
			return nil, ErrCoolifyPrivateResolve
		}
		if !privateCoolifyAddressesAllowed(addresses) {
			return nil, ErrCoolifyPrivateAddress
		}
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		var classified error
		for _, resolved := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			current := classifyPrivateCoolifyDialError(dialErr)
			if classified == nil {
				classified = current
			} else if classified != current {
				classified = ErrCoolifyPrivateConnect
			}
		}
		if classified == nil {
			classified = ErrCoolifyPrivateConnect
		}
		return nil, classified
	}
}

func classifyPrivateCoolifyDialError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrCoolifyPrivateTimeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return ErrCoolifyPrivateTimeout
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ErrCoolifyPrivateRefused
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return ErrCoolifyPrivateRoute
	}
	return ErrCoolifyPrivateConnect
}

func closedCoolifyTransportError(err error) error {
	for _, detail := range []error{
		ErrCoolifyPrivateTarget,
		ErrCoolifyPrivateResolve,
		ErrCoolifyPrivateAddress,
		ErrCoolifyPrivateRefused,
		ErrCoolifyPrivateTimeout,
		ErrCoolifyPrivateRoute,
		ErrCoolifyPrivateConnect,
	} {
		if errors.Is(err, detail) {
			return errors.Join(ErrCoolifyRequestTransport, detail)
		}
	}
	return ErrCoolifyRequestTransport
}

type application struct {
	UUID          string `json:"uuid"`
	Name          string `json:"name"`
	FQDN          string `json:"fqdn"`
	Domain        string `json:"domain"`
	Repository    string `json:"repository"`
	GitRepository string `json:"git_repository"`
	Branch        string `json:"branch"`
	GitBranch     string `json:"git_branch"`
	Description   string `json:"description"`
}

func (a application) domains() string {
	if strings.TrimSpace(a.FQDN) != "" {
		return strings.TrimSpace(a.FQDN)
	}
	return strings.TrimSpace(a.Domain)
}

func (a application) repository() string {
	if strings.TrimSpace(a.GitRepository) != "" {
		return strings.TrimSpace(a.GitRepository)
	}
	return strings.TrimSpace(a.Repository)
}

func (a application) branch() string {
	if strings.TrimSpace(a.GitBranch) != "" {
		return strings.TrimSpace(a.GitBranch)
	}
	return strings.TrimSpace(a.Branch)
}

type environmentEntry struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Comment     string `json:"comment"`
	IsPreview   bool   `json:"is_preview"`
	IsLiteral   bool   `json:"is_literal"`
	IsRuntime   bool   `json:"is_runtime"`
	IsBuildtime bool   `json:"is_buildtime"`
}

type deployment struct {
	UUID           string `json:"uuid"`
	DeploymentUUID string `json:"deployment_uuid"`
	Status         string `json:"status"`
}

func NewClient(config Config) (*Client, error) {
	config.CoolifyURL = strings.TrimRight(strings.TrimSpace(config.CoolifyURL), "/")
	config.CoolifyToken = strings.TrimSpace(config.CoolifyToken)
	for name, value := range map[string]string{
		"Coolify URL": config.CoolifyURL, "Coolify token": config.CoolifyToken,
		"coordinator app id": config.CoordinatorAppID, "front app id": config.FrontAppID,
		"backend app id": config.BackendAppID, "expected front commit": config.ExpectedFrontCommit,
		"expected backend commit": config.ExpectedBackendCommit, "expected protocol": config.ExpectedProtocol,
		"expected catalog hash": config.ExpectedCatalogHash,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	parsed, privateGateway, err := validateCoolifyOrigin(config.CoolifyURL)
	if err != nil {
		return nil, err
	}
	if !commitPattern.MatchString(config.ExpectedFrontCommit) || !commitPattern.MatchString(config.ExpectedBackendCommit) || !protocolPattern.MatchString(config.ExpectedProtocol) || !catalogPattern.MatchString(config.ExpectedCatalogHash) {
		return nil, errors.New("managed compatibility identity is invalid")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if privateGateway {
		config.CoolifyURL = "http://" + parsed.Host
		transport.DialContext = privateCoolifyDialContext(parsed.Host)
	} else {
		config.CoolifyURL = strings.TrimRight(parsed.String(), "/")
	}
	probeTransport := http.DefaultTransport.(*http.Transport).Clone()
	probeTransport.Proxy = nil
	probeTransport.ForceAttemptHTTP2 = false
	probeTransport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &Client{
		config:    config,
		http:      &http.Client{Transport: transport, Timeout: 30 * time.Second},
		probeHTTP: &http.Client{Transport: probeTransport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		sleep:     time.Sleep,
	}, nil
}

func (c *Client) Topology(ctx context.Context) (Topology, error) {
	front, err := c.application(ctx, c.config.FrontAppID)
	if err != nil {
		return Topology{}, errors.Join(ErrTopologyFrontApplication, err)
	}
	backend, err := c.application(ctx, c.config.BackendAppID)
	if err != nil {
		return Topology{}, errors.Join(ErrTopologyBackendApplication, err)
	}
	if front.branch() != "front-door-stable" || backend.branch() != "main" || !managedRepositoryMatches(front.repository()) || !managedRepositoryMatches(backend.repository()) {
		return Topology{}, ErrTopologyManagedIdentity
	}
	frontBackend, err := c.frontBackendURL(ctx)
	if err != nil {
		return Topology{}, ErrTopologyFrontBackend
	}
	return Topology{FrontDomain: front.domains(), FrontBackendURL: frontBackend, BackendDomains: backend.domains()}, nil
}

func (c *Client) SetBackendDomains(ctx context.Context, domains string) (string, error) {
	if !allowedBackendDomains(domains) {
		return "", errors.New("backend domain transition is outside the fixed contract")
	}
	if err := c.patch(ctx, c.config.BackendAppID, map[string]any{"domains": domains}); err != nil {
		return "", err
	}
	return c.deployAndWait(ctx, c.config.BackendAppID)
}

func (c *Client) ConfigureFront(ctx context.Context, domain, backend string) (string, error) {
	if domain != FrontPublicOrigin && domain != FrontTemporaryOrigin {
		return "", errors.New("front domain transition is outside the fixed contract")
	}
	if backend != FrontPublicOrigin && backend != BackendOrigin {
		return "", errors.New("front backend transition is outside the fixed contract")
	}
	if err := c.patch(ctx, c.config.FrontAppID, map[string]any{"domains": domain}); err != nil {
		return "", err
	}
	vars := map[string]string{
		"MCP_FRONT_DOOR_BACKEND_URL":           backend,
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":     c.config.ExpectedProtocol,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH": c.config.ExpectedCatalogHash,
	}
	if err := c.setEnvironment(ctx, c.config.FrontAppID, vars); err != nil {
		return "", err
	}
	return c.deployAndWait(ctx, c.config.FrontAppID)
}

func (c *Client) ProbeBackend(ctx context.Context, origin string) error {
	if origin != FrontPublicOrigin && origin != BackendOrigin {
		return errors.New("backend probe origin is outside the fixed contract")
	}
	return c.probe(ctx, origin, false)
}

func (c *Client) ProbeFront(ctx context.Context, origin string) error {
	if origin != FrontPublicOrigin && origin != FrontTemporaryOrigin {
		return errors.New("front probe origin is outside the fixed contract")
	}
	return c.probe(ctx, origin, true)
}

func (c *Client) PublishStatus(ctx context.Context, status Status) error {
	description, err := encodePublishedStatus(status)
	if err != nil {
		return err
	}
	return c.patch(ctx, c.config.CoordinatorAppID, map[string]any{"description": description})
}

func DecodePublishedStatus(description string) (Status, bool, error) {
	description = strings.TrimSpace(description)
	if strings.HasPrefix(description, compactStatusDescriptionPrefix) {
		status, err := decodeCompactPublishedStatus(strings.TrimPrefix(description, compactStatusDescriptionPrefix))
		return status, true, err
	}
	if !strings.HasPrefix(description, statusDescriptionPrefix) {
		return Status{}, false, nil
	}
	// The v1 shape remains readable for existing coordinator descriptions. New
	// publications always use the compact v2 envelope.
	var status Status
	if err := json.Unmarshal([]byte(strings.TrimPrefix(description, statusDescriptionPrefix)), &status); err != nil {
		return Status{}, true, err
	}
	if err := validateStatus(status); err != nil {
		return Status{}, true, fmt.Errorf("invalid published coordinator status: %w", err)
	}
	return status, true, nil
}

func allowedBackendDomains(raw string) bool {
	normalized := normalizeDomains(raw)
	return normalized == FrontPublicOrigin || normalized == BackendOrigin || normalized == normalizeDomains(FrontPublicOrigin+","+BackendOrigin)
}

func (c *Client) application(ctx context.Context, appID string) (application, error) {
	var app application
	if err := c.requestJSON(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID), nil, &app); err != nil {
		return application{}, err
	}
	if app.UUID == "" {
		app.UUID = appID
	}
	if app.UUID != appID {
		return application{}, ErrCoolifyIdentity
	}
	return app, nil
}

func (c *Client) patch(ctx context.Context, appID string, payload map[string]any) error {
	return c.requestJSON(ctx, http.MethodPatch, "/api/v1/applications/"+url.PathEscape(appID), payload, nil)
}

func (c *Client) setEnvironment(ctx context.Context, appID string, vars map[string]string) error {
	entries, err := c.environmentEntries(ctx, appID)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, entry := range entries {
		if !entry.IsPreview {
			counts[entry.Key]++
		}
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := vars[key]
		if counts[key] > 1 {
			return fmt.Errorf("coolify environment key %s is ambiguous", key)
		}
		method := http.MethodPost
		if counts[key] == 1 {
			method = http.MethodPatch
		}
		payload := map[string]any{
			"key": key, "value": value,
			"comment":    ManagedEnvironmentComment(c.config.CoolifyToken, key, value),
			"is_preview": false, "is_literal": true,
			"is_runtime": true, "is_buildtime": false,
		}
		if err := c.requestJSON(ctx, method, "/api/v1/applications/"+url.PathEscape(appID)+"/envs", payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) deployAndWait(ctx context.Context, appID string) (string, error) {
	var raw json.RawMessage
	path := "/api/v1/deploy?" + url.Values{"uuid": {appID}, "force": {"false"}}.Encode()
	if err := c.requestJSON(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return "", err
	}
	response := decodeDeploymentResponse(raw)
	deploymentID := response.DeploymentUUID
	if deploymentID == "" {
		return "", errors.New("coolify deployment returned no id")
	}
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		var current deployment
		if err := c.requestJSON(ctx, http.MethodGet, "/api/v1/deployments/"+url.PathEscape(deploymentID), nil, &current); err != nil {
			return deploymentID, err
		}
		switch current.Status {
		case "finished":
			return deploymentID, nil
		case "failed", "cancelled":
			return deploymentID, fmt.Errorf("coolify deployment ended in %s", current.Status)
		}
		select {
		case <-ctx.Done():
			return deploymentID, ctx.Err()
		default:
		}
		c.sleep(2 * time.Second)
	}
	return deploymentID, errors.New("coolify deployment did not reach terminal state")
}

func (c *Client) stopAndWait(ctx context.Context, appID string) error {
	if err := c.requestJSON(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID)+"/stop", nil, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var application struct {
			Status string `json:"status"`
		}
		if err := c.requestJSON(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID), nil, &application); err != nil {
			return err
		}
		status := strings.TrimSpace(application.Status)
		if status == "stopped" || status == "exited" || strings.HasPrefix(status, "exited:") {
			return nil
		}
		if status == "" || (!strings.HasPrefix(status, "running:") && status != "running" && status != "stopping") {
			return errors.New("managed application returned an invalid stop state")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c.sleep(2 * time.Second)
	}
	return errors.New("managed application did not stop before deployment")
}

func (c *Client) requestJSON(ctx context.Context, method, path string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return ErrCoolifyRequestBuild
		}
		body = strings.NewReader(string(data))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.config.CoolifyURL+path, body)
	if err != nil {
		return ErrCoolifyRequestBuild
	}
	request.Header.Set("Authorization", "Bearer "+c.config.CoolifyToken)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return closedCoolifyTransportError(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return ErrCoolifyResponseRead
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ErrCoolifyResponseHTTP
	}
	if result != nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, result); err != nil {
			return ErrCoolifyResponseDecode
		}
	}
	return nil
}

func (c *Client) probe(ctx context.Context, origin string, front bool) error {
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := c.probeOnce(ctx, origin, front); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c.sleep(time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("managed origin probe timed out")
	}
	return lastErr
}

func (c *Client) probeOnce(ctx context.Context, origin string, front bool) error {
	client := c.probeHTTP
	if client == nil {
		return errors.New("front-door probe client is not configured")
	}
	if front {
		if _, _, err := getManagedOrigin(ctx, client, origin, "/front-door/healthz"); err != nil {
			return err
		}
		if _, _, err := getManagedOrigin(ctx, client, origin, "/front-door/readyz"); err != nil {
			return err
		}
		body, headers, err := getManagedOrigin(ctx, client, origin, "/front-door/version")
		if err != nil {
			return err
		}
		var info struct {
			Status       string `json:"status"`
			Commit       string `json:"commit"`
			BackendReady bool   `json:"backend_ready"`
			Backend      struct {
				Status    string `json:"status"`
				Protocol  string `json:"protocol_version"`
				Commit    string `json:"commit"`
				Catalog   string `json:"catalog_hash"`
				ToolCount int    `json:"tool_count"`
			} `json:"backend"`
		}
		if json.Unmarshal(body, &info) != nil || info.Status != "ok" || info.Commit != c.config.ExpectedFrontCommit || !info.BackendReady || info.Backend.Status != "ok" || info.Backend.Protocol != c.config.ExpectedProtocol || info.Backend.Catalog != c.config.ExpectedCatalogHash || info.Backend.Commit != c.config.ExpectedBackendCommit || info.Backend.ToolCount < 1 || headers.Get("X-MCP-Front-Door-Commit") != c.config.ExpectedFrontCommit {
			return errors.New("front-door identity does not match the managed contract")
		}
		body, headers, err = getManagedOrigin(ctx, client, origin, "/version")
		if err != nil {
			return err
		}
		return validateBackendIdentity(body, headers, c.config.ExpectedProtocol, c.config.ExpectedCatalogHash, c.config.ExpectedBackendCommit, c.config.ExpectedFrontCommit)
	}
	if _, _, err := getManagedOrigin(ctx, client, origin, "/readyz"); err != nil {
		return err
	}
	body, headers, err := getManagedOrigin(ctx, client, origin, "/version")
	if err != nil {
		return err
	}
	return validateBackendIdentity(body, headers, c.config.ExpectedProtocol, c.config.ExpectedCatalogHash, c.config.ExpectedBackendCommit, "")
}

func getManagedOrigin(ctx context.Context, client *http.Client, origin, path string) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(origin, "/")+path, nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" {
		return nil, nil, fmt.Errorf("managed origin path %s returned HTTP %d or a redirect", path, response.StatusCode)
	}
	return body, response.Header.Clone(), nil
}

func validateBackendIdentity(body []byte, headers http.Header, protocol, catalog, expectedCommit, frontCommit string) error {
	var info struct {
		Status    string `json:"status"`
		Protocol  string `json:"protocol_version"`
		Commit    string `json:"commit"`
		Catalog   string `json:"catalog_hash"`
		ToolCount int    `json:"tool_count"`
	}
	if json.Unmarshal(body, &info) != nil || info.Status != "ok" || info.Protocol != protocol || info.Catalog != catalog || info.Commit != expectedCommit || info.ToolCount < 1 || headers.Get("X-MCP-Server-Commit") != info.Commit || headers.Get("X-MCP-Catalog-Hash") != info.Catalog {
		return errors.New("backend identity does not match the managed contract")
	}
	if frontCommit != "" && headers.Get("X-MCP-Front-Door-Commit") != frontCommit {
		return errors.New("proxied backend front-door identity header mismatch")
	}
	return nil
}

func decodeDeploymentResponse(raw []byte) deployment {
	var direct struct {
		DeploymentUUID string       `json:"deployment_uuid"`
		UUID           string       `json:"uuid"`
		Status         string       `json:"status"`
		Deployments    []deployment `json:"deployments"`
	}
	if json.Unmarshal(raw, &direct) != nil {
		return deployment{}
	}
	if direct.DeploymentUUID == "" {
		direct.DeploymentUUID = direct.UUID
	}
	if direct.DeploymentUUID != "" || direct.Status != "" {
		return deployment{DeploymentUUID: direct.DeploymentUUID, Status: direct.Status}
	}
	if len(direct.Deployments) == 0 {
		return deployment{}
	}
	item := direct.Deployments[0]
	if item.DeploymentUUID == "" {
		item.DeploymentUUID = item.UUID
	}
	return item
}

func managedRepositoryMatches(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimSuffix(raw, ".git")
	raw = strings.TrimPrefix(raw, "https://github.com/")
	raw = strings.TrimPrefix(raw, "http://github.com/")
	raw = strings.TrimPrefix(raw, "ssh://git@github.com/")
	raw = strings.TrimPrefix(raw, "git@github.com:")
	return raw == strings.ToLower(ManagedRepository) || raw == strings.ToLower(ManagedCompatibilityRepository)
}

func (c *Client) frontBackendURL(ctx context.Context) (string, error) {
	entries, err := c.environmentEntries(ctx, c.config.FrontAppID)
	if err != nil {
		return "", err
	}
	var matched *environmentEntry
	for _, entry := range entries {
		if entry.IsPreview || entry.Key != "MCP_FRONT_DOOR_BACKEND_URL" {
			continue
		}
		if matched != nil {
			return "", errors.New("front-door backend environment is ambiguous")
		}
		copy := entry
		matched = &copy
	}
	if matched == nil || !matched.IsLiteral || !matched.IsRuntime || matched.IsBuildtime {
		return "", errors.New("front-door backend environment metadata is outside the fixed contract")
	}
	value, err := ManagedEnvironmentValue(matched.Comment, c.config.CoolifyToken, matched.Key, FrontPublicOrigin, BackendOrigin)
	if err != nil {
		return "", fmt.Errorf("front-door backend environment is outside the fixed contract: %w", err)
	}
	return value, nil
}

func (c *Client) environmentEntries(ctx context.Context, appID string) ([]environmentEntry, error) {
	var raw json.RawMessage
	if err := c.requestJSON(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(appID)+"/envs", nil, &raw); err != nil {
		return nil, err
	}
	return decodeCollection[environmentEntry](raw)
}

func decodeCollection[T any](raw []byte) ([]T, error) {
	var direct []T
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil || wrapped.Data == nil {
		return nil, errors.New("unexpected coolify collection response")
	}
	return wrapped.Data, nil
}
