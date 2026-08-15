package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/catalogidentity"
	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const (
	managedBackendRepository       = "mcp-devbox"
	managedBackendBranch           = "main"
	managedBackendRollbackBranch   = frontdoorcoordinator.ManagedBackendRollbackBranch
	managedBackendManifestPath     = "deploy/catalog-identity.json"
	managedBackendRolloutMarker    = "catalog-aware"
	managedCatalogRequestEnv       = "MCP_FRONT_DOOR_CATALOG_ROLLOUT_REQUEST"
	managedCatalogMCPTokenEnv      = "MCP_FRONT_DOOR_CATALOG_MCP_TOKEN"
	maxManagedCatalogManifestBytes = 16 << 10
)

type managedBackendRolloutIdentity struct {
	Previous      catalogrollout.Identity
	Candidate     catalogrollout.Identity
	FrontCommit   string
	FrontID       string
	CoordinatorID string
	Evidence      githubCheckSummary
	RollbackSHA   string
	RollbackRef   bool
}

func (c *GitHubClient) managedRollbackBranch(ctx context.Context) (string, bool, error) {
	path := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(managedBackendRepository) + "/git/ref/heads/" + url.PathEscape(managedBackendRollbackBranch)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, path, nil, githubRefAndMergeResponseLimit)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	if status < 200 || status >= 300 {
		return "", false, fmt.Errorf("GitHub managed rollback branch lookup -> HTTP %d", status)
	}
	var response githubRefResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil || !frontDoorCommitPattern.MatchString(response.Object.SHA) {
		return "", false, errors.New("GitHub managed rollback branch is invalid")
	}
	return response.Object.SHA, true, nil
}

func (c *GitHubClient) ensureManagedRollbackBranch(ctx context.Context, expectedSHA string, expectedExists bool, targetSHA string) error {
	current, exists, err := c.managedRollbackBranch(ctx)
	if err != nil {
		return err
	}
	if exists != expectedExists || current != expectedSHA {
		return errors.New("GitHub managed rollback branch changed after deployment preview")
	}
	basePath := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(managedBackendRepository) + "/git"
	if !exists {
		payload, _ := json.Marshal(map[string]string{"ref": "refs/heads/" + managedBackendRollbackBranch, "sha": targetSHA})
		status, _, err := c.doJSONLimit(ctx, http.MethodPost, basePath+"/refs", payload, githubRefAndMergeResponseLimit)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("GitHub managed rollback branch creation -> HTTP %d", status)
		}
	} else if current != targetSHA {
		ancestor, err := c.commitIsAncestor(ctx, managedBackendRepository, current, targetSHA)
		if err != nil {
			return err
		}
		if !ancestor {
			return errors.New("GitHub managed rollback branch cannot fast-forward to the running backend")
		}
		payload, _ := json.Marshal(map[string]any{"sha": targetSHA, "force": false})
		status, _, err := c.doJSONLimit(ctx, http.MethodPatch, basePath+"/refs/heads/"+url.PathEscape(managedBackendRollbackBranch), payload, githubRefAndMergeResponseLimit)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("GitHub managed rollback branch update -> HTTP %d", status)
		}
	}
	resolved, present, err := c.managedRollbackBranch(ctx)
	if err != nil || !present || resolved != targetSHA {
		return errors.New("GitHub managed rollback branch did not reach the running backend")
	}
	return nil
}

type githubRepositoryContent struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	SHA      string `json:"sha"`
	Size     int    `json:"size"`
}

func escapeGitHubContentPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (c *GitHubClient) repositoryFileAtRef(ctx context.Context, repo, path, ref string) ([]byte, error) {
	if err := c.configError(); err != nil {
		return nil, err
	}
	if !safeCloneDir(repo) || strings.TrimSpace(path) == "" || !frontDoorCommitPattern.MatchString(strings.TrimSpace(ref)) {
		return nil, errors.New("GitHub content request is invalid")
	}
	endpoint := "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(repo) + "/contents/" + escapeGitHubContentPath(path) + "?ref=" + url.QueryEscape(ref)
	status, body, err := c.doJSONLimit(ctx, http.MethodGet, endpoint, nil, maxManagedCatalogManifestBytes*4)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("GitHub catalog manifest -> HTTP %d", status)
	}
	var response githubRepositoryContent
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return nil, errors.New("GitHub catalog manifest response is invalid")
	}
	if response.Type != "file" || response.Encoding != "base64" || response.Size < 1 || response.Size > maxManagedCatalogManifestBytes || !frontDoorCommitPattern.MatchString(response.SHA) {
		return nil, errors.New("GitHub catalog manifest metadata is invalid")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(response.Content, "\n", ""))
	if err != nil || len(decoded) != response.Size || len(decoded) > maxManagedCatalogManifestBytes {
		return nil, errors.New("GitHub catalog manifest content is invalid")
	}
	return decoded, nil
}

type managedRuntimeIdentityResponse struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at,omitempty"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

func managedIdentityHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ForceAttemptHTTP2 = false
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func decodeManagedRuntimeIdentity(body []byte, headers http.Header) (catalogrollout.Identity, error) {
	var info managedRuntimeIdentityResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&info); err != nil {
		return catalogrollout.Identity{}, errors.New("managed backend identity is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return catalogrollout.Identity{}, errors.New("managed backend identity has trailing data")
	}
	identity := catalogrollout.Identity{Commit: info.Commit, ProtocolVersion: info.ProtocolVersion, ToolCount: info.ToolCount, CatalogHash: info.CatalogHash}
	if info.Status != "ok" || headers.Get("X-MCP-Server-Commit") != identity.Commit || headers.Get("X-MCP-Catalog-Hash") != identity.CatalogHash {
		return catalogrollout.Identity{}, errors.New("managed backend identity headers do not match")
	}
	if err := identity.Validate(); err != nil {
		return catalogrollout.Identity{}, err
	}
	return identity, nil
}

func readManagedRuntimeIdentity(ctx context.Context, origin string) (catalogrollout.Identity, error) {
	if origin != managedFrontDoorBackendOrigin {
		return catalogrollout.Identity{}, errors.New("managed runtime origin is not fixed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/version", nil)
	if err != nil {
		return catalogrollout.Identity{}, err
	}
	response, err := managedIdentityHTTPClient().Do(request)
	if err != nil {
		return catalogrollout.Identity{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" {
		return catalogrollout.Identity{}, errors.New("managed backend identity is unavailable")
	}
	return decodeManagedRuntimeIdentity(body, response.Header)
}

func (s *PlatformCapability) managedBackendRolloutIdentity(ctx context.Context, app platformApplication) (managedBackendRolloutIdentity, error) {
	if app.UUID != managedBackendAppUUID || !s.managedFrontDoorRepositoryMatches(app.repo()) || app.branch() != managedBackendBranch {
		return managedBackendRolloutIdentity{}, errors.New("managed backend application identity is invalid")
	}
	if err := s.github.configError(); err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	candidateCommit, err := s.github.branchSHA(ctx, managedBackendRepository, managedBackendBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(candidateCommit) {
		return managedBackendRolloutIdentity{}, errors.New("managed backend candidate commit is invalid")
	}
	manifestBody, err := s.github.repositoryFileAtRef(ctx, managedBackendRepository, managedBackendManifestPath, candidateCommit)
	if err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	manifest, err := catalogidentity.DecodeManifest(manifestBody)
	if err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	evidence, err := s.github.collectGitHubEvidence(ctx, managedBackendRepository, candidateCommit, managedBackendBranch)
	if err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	if !evidence.EvidenceComplete || !evidence.AllChecksGreen || evidence.Pending != 0 || evidence.Failed != 0 {
		return managedBackendRolloutIdentity{}, errors.New("managed backend candidate does not have complete exact-head green checks")
	}
	previous, err := readManagedRuntimeIdentity(ctx, managedFrontDoorBackendOrigin)
	if err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	candidate := catalogrollout.Identity{Commit: candidateCommit, ProtocolVersion: manifest.ProtocolVersion, ToolCount: manifest.ToolCount, CatalogHash: manifest.CatalogHash}
	if err := candidate.Validate(); err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	if previous.ProtocolVersion != candidate.ProtocolVersion {
		return managedBackendRolloutIdentity{}, errors.New("managed backend rollout cannot change the MCP protocol")
	}
	rollbackSHA, rollbackExists, err := s.github.managedRollbackBranch(ctx)
	if err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	if rollbackExists && rollbackSHA != previous.Commit {
		ancestor, err := s.github.commitIsAncestor(ctx, managedBackendRepository, rollbackSHA, previous.Commit)
		if err != nil {
			return managedBackendRolloutIdentity{}, err
		}
		if !ancestor {
			return managedBackendRolloutIdentity{}, errors.New("managed backend rollback branch is not an ancestor of the running backend")
		}
	}
	frontCommit, err := s.github.branchSHA(ctx, managedBackendRepository, managedFrontDoorBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(frontCommit) {
		return managedBackendRolloutIdentity{}, errors.New("stable front-door commit is invalid")
	}
	if err := probeManagedOriginOnce(managedFrontDoorPublicOrigin, true, frontCommit, previous.ProtocolVersion, previous.CatalogHash); err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	front, exists, err := s.managedFrontDoorApp()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("managed front-door application is absent")
		}
		return managedBackendRolloutIdentity{}, err
	}
	coordinator, exists, err := s.managedFrontDoorCoordinatorApp()
	if err != nil || !exists {
		if err == nil {
			err = errors.New("managed front-door coordinator is absent")
		}
		return managedBackendRolloutIdentity{}, err
	}
	if err := s.validateManagedFrontDoorCoordinatorApp(coordinator); err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	if _, err := s.verifyManagedFrontDoorCoordinatorStorage(coordinator.UUID); err != nil {
		return managedBackendRolloutIdentity{}, err
	}
	if status, present, err := catalogrollout.DecodePublishedStatus(coordinator.Description); err != nil {
		return managedBackendRolloutIdentity{}, err
	} else if present && (status.State == catalogrollout.StateQueued || status.State == catalogrollout.StateRunning || status.State == catalogrollout.StateCompensating) {
		return managedBackendRolloutIdentity{}, errors.New("a catalog-aware backend rollout is already active")
	}
	if status, present, err := frontdoorcoordinator.DecodePublishedStatus(coordinator.Description); err != nil {
		return managedBackendRolloutIdentity{}, err
	} else if present && (status.State == frontdoorcoordinator.StateQueued || status.State == frontdoorcoordinator.StateRunning || status.State == frontdoorcoordinator.StateCompensating) {
		return managedBackendRolloutIdentity{}, errors.New("a front-door topology transition is active")
	}
	return managedBackendRolloutIdentity{Previous: previous, Candidate: candidate, FrontCommit: frontCommit, FrontID: front.UUID, CoordinatorID: coordinator.UUID, Evidence: evidence, RollbackSHA: rollbackSHA, RollbackRef: rollbackExists}, nil
}

func rolloutIdentityArgs(identity managedBackendRolloutIdentity) map[string]string {
	return map[string]string{
		"rollout":                managedBackendRolloutMarker,
		"previous_commit":        identity.Previous.Commit,
		"previous_protocol":      identity.Previous.ProtocolVersion,
		"previous_tool_count":    fmt.Sprint(identity.Previous.ToolCount),
		"previous_catalog_hash":  identity.Previous.CatalogHash,
		"candidate_commit":       identity.Candidate.Commit,
		"candidate_protocol":     identity.Candidate.ProtocolVersion,
		"candidate_tool_count":   fmt.Sprint(identity.Candidate.ToolCount),
		"candidate_catalog_hash": identity.Candidate.CatalogHash,
		"front_commit":           identity.FrontCommit,
		"front_app":              identity.FrontID,
		"rollback_branch_sha":    identity.RollbackSHA,
		"rollback_branch_exists": fmt.Sprint(identity.RollbackRef),
		"coordinator_app":        identity.CoordinatorID,
		"checks_source":          identity.Evidence.Source,
		"checks_passed":          fmt.Sprint(identity.Evidence.Passed),
	}
}

func identityFromArgs(args map[string]string, prefix string) (catalogrollout.Identity, error) {
	identity := catalogrollout.Identity{Commit: args[prefix+"_commit"], ProtocolVersion: args[prefix+"_protocol"], ToolCount: parseCount(args[prefix+"_tool_count"]), CatalogHash: args[prefix+"_catalog_hash"]}
	return identity, identity.Validate()
}

func (s *PlatformCapability) managedBackendRolloutPreview(app platformApplication) (string, error) {
	identity, err := s.managedBackendRolloutIdentity(context.Background(), app)
	if err != nil {
		return "", err
	}
	args := rolloutIdentityArgs(identity)
	args["app"] = app.UUID
	args["name"] = app.Name
	args["repository"] = app.repo()
	args["branch"] = app.branch()
	args["app_commit"] = app.commit()
	plan, err := s.plans.Create("platform-deploy", args)
	if err != nil {
		return "", err
	}
	changed := identity.Previous.CatalogHash != identity.Candidate.CatalogHash
	return fmt.Sprintf("app: %s\nname: %s\nrepository: %s\nbranch: %s\nprevious_commit: %s\ncandidate_commit: %s\nprotocol: %s\nprevious_tool_count: %d\ncandidate_tool_count: %d\nprevious_catalog_hash: %s\ncandidate_catalog_hash: %s\ncatalog_changed: %t\nchecks_source: %s\nchecks_passed: %d\nrollback_branch: %s\nrollback_branch_sha: %s\ncoordinator_application_uuid: %s\neffect: fast-forward the fixed rollback branch to the running backend, then execute one durable stop-first catalog-aware rollout; direct Coolify deployment is forbidden\nplan_id: %s\nexpiry: %s\n",
		app.UUID, app.Name, app.repo(), app.branch(), identity.Previous.Commit, identity.Candidate.Commit,
		identity.Candidate.ProtocolVersion, identity.Previous.ToolCount, identity.Candidate.ToolCount,
		identity.Previous.CatalogHash, identity.Candidate.CatalogHash, changed, identity.Evidence.Source,
		identity.Evidence.Passed, managedBackendRollbackBranch, identity.RollbackSHA, identity.CoordinatorID, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *PlatformCapability) executeManagedBackendRollout(plan ActionPlan) (string, error) {
	if strings.TrimSpace(s.managedMCPToken) == "" {
		return "", errors.New("managed backend rollout MCP smoke token is unavailable")
	}
	app, err := s.getPlatformApp(plan.Args["app"])
	if err != nil {
		return "", err
	}
	if app.UUID != managedBackendAppUUID || app.repo() != plan.Args["repository"] || app.branch() != plan.Args["branch"] || app.commit() != plan.Args["app_commit"] {
		return "", errors.New("managed backend application changed after deployment preview")
	}
	identity, err := s.managedBackendRolloutIdentity(context.Background(), app)
	if err != nil {
		return "", err
	}
	previous, err := identityFromArgs(plan.Args, "previous")
	if err != nil {
		return "", err
	}
	candidate, err := identityFromArgs(plan.Args, "candidate")
	if err != nil {
		return "", err
	}
	if identity.Previous != previous || identity.Candidate != candidate || identity.FrontCommit != plan.Args["front_commit"] || identity.FrontID != plan.Args["front_app"] || identity.CoordinatorID != plan.Args["coordinator_app"] || identity.Evidence.Source != plan.Args["checks_source"] || fmt.Sprint(identity.Evidence.Passed) != plan.Args["checks_passed"] || identity.RollbackSHA != plan.Args["rollback_branch_sha"] || fmt.Sprint(identity.RollbackRef) != plan.Args["rollback_branch_exists"] {
		return "", errors.New("managed backend rollout identity changed after preview")
	}
	if err := s.github.ensureManagedRollbackBranch(context.Background(), identity.RollbackSHA, identity.RollbackRef, previous.Commit); err != nil {
		return "", err
	}
	coordinatorCoolifyURL, err := s.managedFrontDoorCoordinatorCoolifyURL()
	if err != nil {
		return "", err
	}
	status, body, err := s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(app.UUID), map[string]any{
		"git_commit_sha": previous.Commit, "is_auto_deploy_enabled": false, "instant_deploy": false,
	})
	if err != nil || status < 200 || status >= 300 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("managed backend deployment gate -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	status, body, err = s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(identity.CoordinatorID), map[string]any{
		"git_commit_sha": candidate.Commit, "is_auto_deploy_enabled": false, "instant_deploy": false,
	})
	if err != nil || status < 200 || status >= 300 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("managed coordinator commit pin -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	requestBody, err := json.Marshal(catalogrollout.Request{RequestID: plan.ID, Previous: previous, Candidate: candidate})
	if err != nil {
		return "", err
	}
	vars := map[string]string{
		"COOLIFY_URL":                            coordinatorCoolifyURL,
		"COOLIFY_API_TOKEN":                      s.coolify.token,
		"MCP_FRONT_DOOR_COORDINATOR_APP_UUID":    identity.CoordinatorID,
		"MCP_FRONT_DOOR_APP_UUID":                identity.FrontID,
		"MCP_FRONT_DOOR_BACKEND_APP_UUID":        managedBackendAppUUID,
		"MCP_FRONT_DOOR_EXPECTED_COMMIT":         identity.FrontCommit,
		"MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT": previous.Commit,
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":       previous.ProtocolVersion,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH":   previous.CatalogHash,
		"MCP_FRONT_DOOR_COORDINATOR_TARGET":      string(frontdoorcoordinator.TargetIdle),
		"MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID":  "",
		"MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT":  managedFrontDoorCoordinatorStateMount,
		"MCP_FRONT_DOOR_COORDINATOR_ADDR":        "0.0.0.0:" + managedFrontDoorCoordinatorPort,
		managedCatalogRequestEnv:                 string(requestBody),
		managedCatalogMCPTokenEnv:                s.managedMCPToken,
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if _, err := s.coolify.setEnvironmentVariables(context.Background(), identity.CoordinatorID, vars, keys); err != nil {
		return "", err
	}
	status, body, err = s.coolify.deploy(context.Background(), identity.CoordinatorID, false)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("managed catalog rollout dispatch -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	deployment := decodePlatformDeployResponse(body)
	return fmt.Sprintf("rollout_dispatched: true\ncoordinator_application_uuid: %s\ncoordinator_deployment_id: %s\ndeployment_status: %s\nprevious_commit: %s\ncandidate_commit: %s\nprevious_catalog_hash: %s\ncandidate_catalog_hash: %s\nauto_deploy_enabled: false\ninstant_deploy: false\n",
		identity.CoordinatorID, deployment.DeploymentUUID, deployment.Status, previous.Commit, candidate.Commit, previous.CatalogHash, candidate.CatalogHash), nil
}
