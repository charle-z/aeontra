package frontdoorcoordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
)

const maxCatalogProbeBody = 1 << 20

const ManagedBackendRollbackBranch = "backend-rollback-stable"

type CatalogPlatform struct {
	client   *Client
	mcpToken string
}

func NewCatalogPlatform(client *Client, mcpToken string) (*CatalogPlatform, error) {
	if client == nil {
		return nil, errors.New("catalog rollout client is required")
	}
	mcpToken = strings.TrimSpace(mcpToken)
	if mcpToken == "" || strings.ContainsAny(mcpToken, "\r\n") {
		return nil, errors.New("catalog rollout MCP token is required")
	}
	return &CatalogPlatform{client: client, mcpToken: mcpToken}, nil
}

type catalogApplication struct {
	UUID                string `json:"uuid"`
	Repository          string `json:"repository"`
	GitRepository       string `json:"git_repository"`
	Branch              string `json:"branch"`
	GitBranch           string `json:"git_branch"`
	GitCommitSHA        string `json:"git_commit_sha"`
	IsAutoDeployEnabled bool   `json:"is_auto_deploy_enabled"`
	InstantDeploy       bool   `json:"instant_deploy"`
	Status              string `json:"status"`
}

func (a catalogApplication) repository() string {
	if strings.TrimSpace(a.GitRepository) != "" {
		return strings.TrimSpace(a.GitRepository)
	}
	return strings.TrimSpace(a.Repository)
}
func (a catalogApplication) branch() string {
	if strings.TrimSpace(a.GitBranch) != "" {
		return strings.TrimSpace(a.GitBranch)
	}
	return strings.TrimSpace(a.Branch)
}

type catalogRuntimeInfo struct {
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

func (p *CatalogPlatform) Observe(ctx context.Context) (catalogrollout.Observation, error) {
	application, err := p.backendApplication(ctx)
	if err != nil {
		return catalogrollout.Observation{}, err
	}
	if application.UUID != p.client.config.BackendAppID || !managedRepositoryMatches(application.repository()) || application.branch() != "main" {
		return catalogrollout.Observation{}, errors.New("managed backend application identity is invalid")
	}
	if application.IsAutoDeployEnabled || application.InstantDeploy {
		return catalogrollout.Observation{}, errors.New("managed backend auto-deploy is not disabled")
	}
	info, err := p.runtimeInfo(ctx, BackendOrigin)
	if err != nil {
		return catalogrollout.Observation{}, err
	}
	if application.GitCommitSHA != info.Commit {
		return catalogrollout.Observation{}, errors.New("managed backend commit pin does not match the running backend")
	}
	front, protocol, err := p.frontContract(ctx)
	if err != nil {
		return catalogrollout.Observation{}, err
	}
	if protocol != info.ProtocolVersion {
		return catalogrollout.Observation{}, errors.New("front-door protocol does not match the running backend")
	}
	return catalogrollout.Observation{
		Backend: catalogrollout.Identity{Commit: info.Commit, ProtocolVersion: info.ProtocolVersion, ToolCount: info.ToolCount, CatalogHash: info.CatalogHash},
		Front:   front,
	}, nil
}

func (p *CatalogPlatform) PrepareFront(ctx context.Context, previous, candidate catalogrollout.Identity) (string, error) {
	if err := validateCatalogPair(previous, candidate); err != nil {
		return "", err
	}
	if err := p.client.setEnvironment(ctx, p.client.config.FrontAppID, map[string]string{
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":       candidate.ProtocolVersion,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH":   candidate.CatalogHash,
		"MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH": previous.CatalogHash,
	}); err != nil {
		return "", err
	}
	deploymentID, err := p.client.deployAndWait(ctx, p.client.config.FrontAppID)
	if err != nil {
		return deploymentID, err
	}
	front, protocol, err := p.frontContract(ctx)
	if err != nil {
		return deploymentID, err
	}
	if protocol != candidate.ProtocolVersion || front.Primary != candidate.CatalogHash || front.Transition != previous.CatalogHash {
		return deploymentID, errors.New("front-door transition contract was not applied")
	}
	if err := p.verifyFront(ctx, previous); err != nil {
		return deploymentID, err
	}
	return deploymentID, nil
}

func (p *CatalogPlatform) DeployBackend(ctx context.Context, candidate catalogrollout.Identity) (string, error) {
	return p.deployBackendFromBranch(ctx, candidate, "main")
}

func (p *CatalogPlatform) deployBackendFromBranch(ctx context.Context, candidate catalogrollout.Identity, branch string) (deploymentID string, resultErr error) {
	if err := candidate.Validate(); err != nil {
		return "", err
	}
	if branch != "main" && branch != ManagedBackendRollbackBranch {
		return "", errors.New("managed backend deployment branch is invalid")
	}
	if err := p.client.patch(ctx, p.client.config.BackendAppID, map[string]any{
		"git_branch": branch, "git_commit_sha": candidate.Commit, "is_auto_deploy_enabled": false, "instant_deploy": false,
	}); err != nil {
		return "", err
	}
	if branch == ManagedBackendRollbackBranch {
		defer func() {
			restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			restoreErr := p.client.patch(restoreCtx, p.client.config.BackendAppID, map[string]any{
				"git_branch": "main", "git_commit_sha": candidate.Commit,
				"is_auto_deploy_enabled": false, "instant_deploy": false,
			})
			resultErr = errors.Join(resultErr, restoreErr)
		}()
	}
	if err := p.client.stopAndWait(ctx, p.client.config.BackendAppID); err != nil {
		return "", err
	}
	return p.client.deployAndWait(ctx, p.client.config.BackendAppID)
}

func (p *CatalogPlatform) VerifyBackend(ctx context.Context, candidate catalogrollout.Identity) error {
	if err := p.poll(ctx, func(ctx context.Context) error {
		info, err := p.runtimeInfo(ctx, BackendOrigin)
		if err != nil {
			return err
		}
		return matchCatalogRuntime(info, candidate)
	}); err != nil {
		return err
	}
	if err := p.poll(ctx, func(ctx context.Context) error { return p.verifyFront(ctx, candidate) }); err != nil {
		return err
	}
	if err := p.verifyOAuth(ctx); err != nil {
		return err
	}
	return p.verifyMCP(ctx, candidate)
}

func (p *CatalogPlatform) FinalizeFront(ctx context.Context, candidate catalogrollout.Identity) (string, error) {
	if err := candidate.Validate(); err != nil {
		return "", err
	}
	if err := p.client.setEnvironment(ctx, p.client.config.FrontAppID, map[string]string{
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":       candidate.ProtocolVersion,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH":   candidate.CatalogHash,
		"MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH": "",
	}); err != nil {
		return "", err
	}
	deploymentID, err := p.client.deployAndWait(ctx, p.client.config.FrontAppID)
	if err != nil {
		return deploymentID, err
	}
	front, protocol, err := p.frontContract(ctx)
	if err != nil {
		return deploymentID, err
	}
	if protocol != candidate.ProtocolVersion || front.Primary != candidate.CatalogHash || front.Transition != "" {
		return deploymentID, errors.New("front-door final catalog contract was not applied")
	}
	if err := p.verifyFront(ctx, candidate); err != nil {
		return deploymentID, err
	}
	return deploymentID, nil
}

func (p *CatalogPlatform) RollbackBackend(ctx context.Context, previous catalogrollout.Identity) (string, error) {
	return p.deployBackendFromBranch(ctx, previous, ManagedBackendRollbackBranch)
}

func (p *CatalogPlatform) RollbackFront(ctx context.Context, previous catalogrollout.Identity) (string, error) {
	return p.FinalizeFront(ctx, previous)
}

func (p *CatalogPlatform) PublishStatus(ctx context.Context, status catalogrollout.Status) error {
	description, err := catalogrollout.EncodePublishedStatus(status)
	if err != nil {
		return err
	}
	return p.client.patch(ctx, p.client.config.CoordinatorAppID, map[string]any{"description": description})
}

func validateCatalogPair(previous, candidate catalogrollout.Identity) error {
	if err := previous.Validate(); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	if previous.ProtocolVersion != candidate.ProtocolVersion {
		return errors.New("catalog rollout cannot change protocol version")
	}
	if previous.CatalogHash == candidate.CatalogHash {
		return errors.New("catalog transition requires two distinct hashes")
	}
	return nil
}

func (p *CatalogPlatform) backendApplication(ctx context.Context) (catalogApplication, error) {
	var application catalogApplication
	if err := p.client.requestJSON(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(p.client.config.BackendAppID), nil, &application); err != nil {
		return catalogApplication{}, err
	}
	return application, nil
}

func (p *CatalogPlatform) frontContract(ctx context.Context) (catalogrollout.FrontContract, string, error) {
	entries, err := p.client.environmentEntries(ctx, p.client.config.FrontAppID)
	if err != nil {
		return catalogrollout.FrontContract{}, "", err
	}
	values := map[string]string{}
	counts := map[string]int{}
	for _, entry := range entries {
		if entry.IsPreview {
			continue
		}
		switch entry.Key {
		case "MCP_FRONT_DOOR_EXPECTED_PROTOCOL", "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH":
		default:
			continue
		}
		counts[entry.Key]++
		if counts[entry.Key] > 1 || !entry.IsLiteral || !entry.IsRuntime || entry.IsBuildtime {
			return catalogrollout.FrontContract{}, "", errors.New("front-door catalog environment is ambiguous or unsafe")
		}
		value, err := ManagedEnvironmentValue(entry.Comment, p.client.config.CoolifyToken, entry.Key)
		if err != nil {
			return catalogrollout.FrontContract{}, "", err
		}
		values[entry.Key] = value
	}
	if counts["MCP_FRONT_DOOR_EXPECTED_PROTOCOL"] != 1 || counts["MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"] != 1 || counts["MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH"] > 1 {
		return catalogrollout.FrontContract{}, "", errors.New("front-door catalog environment is incomplete")
	}
	return catalogrollout.FrontContract{
		Primary:    values["MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"],
		Transition: values["MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH"],
	}, values["MCP_FRONT_DOOR_EXPECTED_PROTOCOL"], nil
}

func (p *CatalogPlatform) runtimeInfo(ctx context.Context, origin string) (catalogRuntimeInfo, error) {
	if _, _, err := getManagedOrigin(ctx, p.client.probeHTTP, origin, "/readyz"); err != nil {
		return catalogRuntimeInfo{}, err
	}
	body, headers, err := getManagedOrigin(ctx, p.client.probeHTTP, origin, "/version")
	if err != nil {
		return catalogRuntimeInfo{}, err
	}
	var info catalogRuntimeInfo
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&info); err != nil {
		return catalogRuntimeInfo{}, errors.New("catalog runtime version is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return catalogRuntimeInfo{}, errors.New("catalog runtime version has trailing data")
	}
	if info.Status != "ok" || headers.Get("X-MCP-Server-Commit") != info.Commit || headers.Get("X-MCP-Catalog-Hash") != info.CatalogHash {
		return catalogRuntimeInfo{}, errors.New("catalog runtime identity headers do not match")
	}
	return info, nil
}

func matchCatalogRuntime(info catalogRuntimeInfo, expected catalogrollout.Identity) error {
	if info.Status != "ok" || info.Commit != expected.Commit || info.ProtocolVersion != expected.ProtocolVersion || info.ToolCount != expected.ToolCount || info.CatalogHash != expected.CatalogHash {
		return errors.New("catalog runtime identity does not match")
	}
	return nil
}

func (p *CatalogPlatform) verifyFront(ctx context.Context, expected catalogrollout.Identity) error {
	if _, _, err := getManagedOrigin(ctx, p.client.probeHTTP, FrontPublicOrigin, "/front-door/readyz"); err != nil {
		return err
	}
	body, headers, err := getManagedOrigin(ctx, p.client.probeHTTP, FrontPublicOrigin, "/front-door/version")
	if err != nil {
		return err
	}
	var info struct {
		Status       string             `json:"status"`
		Commit       string             `json:"commit"`
		BackendReady bool               `json:"backend_ready"`
		Backend      catalogRuntimeInfo `json:"backend"`
	}
	if json.Unmarshal(body, &info) != nil || info.Status != "ok" || info.Commit != p.client.config.ExpectedFrontCommit || !info.BackendReady || headers.Get("X-MCP-Front-Door-Commit") != p.client.config.ExpectedFrontCommit {
		return errors.New("front-door runtime identity is invalid")
	}
	if err := matchCatalogRuntime(info.Backend, expected); err != nil {
		return err
	}
	proxied, err := p.runtimeInfo(ctx, FrontPublicOrigin)
	if err != nil {
		return err
	}
	return matchCatalogRuntime(proxied, expected)
}

func (p *CatalogPlatform) verifyOAuth(ctx context.Context) error {
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server"} {
		body, _, err := getManagedOrigin(ctx, p.client.probeHTTP, FrontPublicOrigin, path)
		if err != nil {
			return err
		}
		var value map[string]any
		if json.Unmarshal(body, &value) != nil || len(value) == 0 {
			return errors.New("OAuth discovery response is invalid")
		}
	}
	return nil
}

func (p *CatalogPlatform) verifyMCP(ctx context.Context, expected catalogrollout.Identity) error {
	endpoint := FrontPublicOrigin + "/mcp"
	sessionID, err := p.mcpRequest(ctx, endpoint, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": expected.ProtocolVersion, "capabilities": map[string]any{}},
	}, expected, false)
	if err != nil {
		return err
	}
	_, err = p.mcpRequest(ctx, endpoint, sessionID, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "system_runtime_info", "arguments": map[string]any{}},
	}, expected, true)
	return err
}

func (p *CatalogPlatform) mcpRequest(ctx context.Context, endpoint, sessionID string, payload map[string]any, expected catalogrollout.Identity, requireIdentityBody bool) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+p.mcpToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := p.client.probeHTTP.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogProbeBody+1))
	if err != nil || len(body) > maxCatalogProbeBody || response.StatusCode != http.StatusOK || response.Header.Get("X-MCP-Catalog-Hash") != expected.CatalogHash || response.Header.Get("X-MCP-Front-Door-Commit") != p.client.config.ExpectedFrontCommit {
		return "", errors.New("managed MCP smoke request failed")
	}
	if requireIdentityBody && (!bytes.Contains(body, []byte(expected.Commit)) || !bytes.Contains(body, []byte(expected.CatalogHash))) {
		return "", errors.New("managed MCP runtime tool returned a different identity")
	}
	if requireIdentityBody && bytes.Contains(body, []byte(`"isError":true`)) {
		return "", errors.New("managed MCP runtime tool returned an error")
	}
	returnedSession := strings.TrimSpace(response.Header.Get("Mcp-Session-Id"))
	if sessionID == "" && returnedSession == "" {
		return "", errors.New("managed MCP initialize returned no session")
	}
	if sessionID != "" && returnedSession != "" && returnedSession != sessionID {
		return "", errors.New("managed MCP session identity changed")
	}
	if sessionID != "" {
		return sessionID, nil
	}
	return returnedSession, nil
}

func (p *CatalogPlatform) poll(ctx context.Context, check func(context.Context) error) error {
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := check(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		p.client.sleep(time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("catalog rollout verification timed out")
	}
	return fmt.Errorf("catalog rollout verification failed: %w", lastErr)
}
