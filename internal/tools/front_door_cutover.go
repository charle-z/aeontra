package tools

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	managedBackendAppUUID           = "jqf7qz5ensoqtvl1tb197gcv"
	managedFrontDoorPublicOrigin    = "https://mcp-devbox-charlez.duckdns.org"
	managedFrontDoorTemporaryOrigin = "https://front.mcp-devbox-charlez.duckdns.org"
	managedFrontDoorLegacyOrigin    = "https://144-225-147-58.sslip.io"
	managedFrontDoorBackendOrigin   = "https://backend.mcp-devbox-charlez.duckdns.org"

	frontDoorActionCreate               = "create"
	frontDoorActionReconcile            = "reconcile"
	frontDoorActionRenameTemporary      = "rename-temporary"
	frontDoorActionCutover              = "cutover"
	frontDoorActionResumeCutoverBackend = "resume-cutover-backend"
	frontDoorActionResumeCutoverPublic  = "resume-cutover-public"
	frontDoorActionRollback             = "rollback"
)

type managedRuntimeInfo struct {
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

type managedFrontDoorInfo struct {
	Status       string             `json:"status"`
	Commit       string             `json:"commit"`
	BackendReady bool               `json:"backend_ready"`
	Backend      managedRuntimeInfo `json:"backend"`
}

func (s *PlatformCapability) managedFrontDoorAction(app platformApplication, request PlatformFrontDoorRequest) (string, platformApplication, error) {
	if err := s.validateManagedFrontDoorApp(app, request.Domain); err != nil {
		return "", platformApplication{}, err
	}
	current := strings.TrimSpace(app.domain())
	if current == "" {
		return frontDoorActionReconcile, platformApplication{}, nil
	}
	if request.BackendURL == managedFrontDoorPublicOrigin {
		switch {
		case request.Domain == managedFrontDoorTemporaryOrigin && current == managedFrontDoorLegacyOrigin:
			return frontDoorActionRenameTemporary, platformApplication{}, nil
		case request.Domain == managedFrontDoorTemporaryOrigin && current == managedFrontDoorPublicOrigin:
			backend, err := s.managedBackendAppOnOrigin(managedFrontDoorBackendOrigin)
			if err != nil {
				return "", platformApplication{}, err
			}
			return frontDoorActionRollback, backend, nil
		case request.Domain == managedFrontDoorTemporaryOrigin && current == managedFrontDoorTemporaryOrigin:
			backend, err := s.managedBackendAppOnOrigin(managedFrontDoorPublicOrigin)
			if err != nil {
				return "", platformApplication{}, err
			}
			return frontDoorActionReconcile, backend, nil
		case request.Domain == managedFrontDoorLegacyOrigin && current == managedFrontDoorLegacyOrigin:
			backend, err := s.managedBackendAppOnOrigin(managedFrontDoorPublicOrigin)
			if err != nil {
				return "", platformApplication{}, err
			}
			return frontDoorActionReconcile, backend, nil
		}
	}
	if request.Domain == managedFrontDoorPublicOrigin && request.BackendURL == managedFrontDoorBackendOrigin {
		switch current {
		case managedFrontDoorLegacyOrigin, managedFrontDoorTemporaryOrigin:
			backend, err := s.managedBackendApp()
			if err != nil {
				return "", platformApplication{}, err
			}
			switch {
			case managedDomainListMatches(backend.domain(), managedFrontDoorPublicOrigin):
				return frontDoorActionCutover, backend, nil
			case managedDomainListMatches(backend.domain(), managedFrontDoorPublicOrigin, managedFrontDoorBackendOrigin):
				return frontDoorActionResumeCutoverBackend, backend, nil
			case managedDomainListMatches(backend.domain(), managedFrontDoorBackendOrigin):
				return frontDoorActionResumeCutoverPublic, backend, nil
			default:
				return "", platformApplication{}, errors.New("managed backend domains do not match a resumable cutover state")
			}
		case managedFrontDoorPublicOrigin:
			backend, err := s.managedBackendAppOnOrigin(managedFrontDoorBackendOrigin)
			if err != nil {
				return "", platformApplication{}, err
			}
			return frontDoorActionReconcile, backend, nil
		}
	}
	if current == request.Domain {
		return frontDoorActionReconcile, platformApplication{}, nil
	}
	return "", platformApplication{}, errors.New("existing front-door application domain does not match an allowed managed transition")
}

func (s *PlatformCapability) managedBackendAppOnOrigin(expected string) (platformApplication, error) {
	backend, err := s.managedBackendApp()
	if err != nil {
		return platformApplication{}, err
	}
	if !managedDomainListMatches(backend.domain(), expected) {
		return platformApplication{}, fmt.Errorf("managed backend is not on expected origin %s", expected)
	}
	return backend, nil
}

func (s *PlatformCapability) managedBackendApp() (platformApplication, error) {
	status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/applications/"+managedBackendAppUUID, nil)
	if err != nil {
		return platformApplication{}, fmt.Errorf("reading managed backend application: %w", err)
	}
	if status < 200 || status >= 300 {
		return platformApplication{}, fmt.Errorf("reading managed backend application -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	var app platformApplication
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		return platformApplication{}, fmt.Errorf("decoding managed backend application: %w", err)
	}
	if app.UUID == "" {
		app.UUID = managedBackendAppUUID
	}
	if app.UUID != managedBackendAppUUID || !s.managedFrontDoorRepositoryMatches(app.repo()) || app.branch() != "main" || !s.managedBackendDomainsAllowed(app.domain()) {
		return platformApplication{}, errors.New("managed backend application does not match the fixed cutover contract")
	}
	return app, nil
}

func (s *PlatformCapability) managedBackendDomainsAllowed(raw string) bool {
	domains := managedDomainList(raw)
	if len(domains) == 0 {
		return false
	}
	for _, domain := range domains {
		if !s.coolify.domainAllowed(domain) {
			return false
		}
	}
	return true
}

func managedDomainList(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	seen := map[string]bool{}
	domains := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			return nil
		}
		seen[part] = true
		domains = append(domains, part)
	}
	return domains
}

func managedDomainListMatches(raw string, expected ...string) bool {
	actual := managedDomainList(raw)
	if len(actual) != len(expected) {
		return false
	}
	want := make(map[string]bool, len(expected))
	for _, domain := range expected {
		domain = strings.TrimSpace(domain)
		if domain == "" || want[domain] {
			return false
		}
		want[domain] = true
	}
	for _, domain := range actual {
		if !want[domain] {
			return false
		}
	}
	return true
}

func (s *PlatformCapability) executeManagedFrontDoorTransition(action string, app, backend platformApplication, request PlatformFrontDoorRequest, sha string) (string, error) {
	switch action {
	case frontDoorActionRenameTemporary:
		deploymentID, err := s.patchAndDeployManagedFrontDoorDomain(app.UUID, managedFrontDoorTemporaryOrigin)
		if err != nil {
			compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, managedFrontDoorLegacyOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
			return "", managedTransitionError("activating temporary DuckDNS front-door routing", err, compensationErr)
		}
		if err := s.probeManagedOrigin(managedFrontDoorTemporaryOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
			compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, managedFrontDoorLegacyOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
			return "", managedTransitionError("temporary DuckDNS front door did not become ready", err, compensationErr)
		}
		return fmt.Sprintf("application_uuid: %s\naction: %s\ndeployment_id: %s\ndomain: %s\nbackend_origin: %s\nfront_door_commit: %s\nrollback_domain: %s\n", app.UUID, action, deploymentID, managedFrontDoorTemporaryOrigin, managedFrontDoorPublicOrigin, sha, managedFrontDoorLegacyOrigin), nil
	case frontDoorActionCutover:
		return s.executeManagedFrontDoorCutover(app, backend, request, sha)
	case frontDoorActionResumeCutoverBackend:
		return s.executeManagedFrontDoorCutoverBackendReady(app, backend, request, sha)
	case frontDoorActionResumeCutoverPublic:
		return s.executeManagedFrontDoorCutoverPublicReady(app, backend, request, sha)
	case frontDoorActionRollback:
		return s.executeManagedFrontDoorRollback(app, backend, request, sha)
	default:
		return "", errors.New("unknown managed front-door transition")
	}
}

func (s *PlatformCapability) executeManagedFrontDoorCutover(app, backend platformApplication, request PlatformFrontDoorRequest, sha string) (string, error) {
	backendOriginDeploymentID, err := s.patchAndStartManagedApplicationDomains(backend.UUID, managedFrontDoorPublicOrigin+","+managedFrontDoorBackendOrigin)
	if err != nil {
		return "", fmt.Errorf("adding managed backend origin: %w", err)
	}
	return fmt.Sprintf("application_uuid: %s\naction: %s\nbackend_origin_deployment_id: %s\nbackend_domains: %s,%s\nnext_action: %s\n", app.UUID, frontDoorActionCutover, backendOriginDeploymentID, managedFrontDoorPublicOrigin, managedFrontDoorBackendOrigin, frontDoorActionResumeCutoverBackend), nil
}

func (s *PlatformCapability) executeManagedFrontDoorCutoverBackendReady(app, backend platformApplication, request PlatformFrontDoorRequest, sha string) (string, error) {
	if err := s.probeManagedOrigin(managedFrontDoorBackendOrigin, false, "", request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		return "", fmt.Errorf("managed backend origin did not become ready: %w", err)
	}
	deploymentID, err := s.configureAndDeployManagedFrontDoor(app.UUID, request)
	if err != nil {
		return "", fmt.Errorf("switching front door to alternate backend: %w", err)
	}
	if err := s.waitManagedDeployment(deploymentID); err != nil {
		return "", err
	}
	currentFrontOrigin := strings.TrimSpace(app.domain())
	if err := s.probeManagedOrigin(currentFrontOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		return "", fmt.Errorf("front door did not accept the alternate backend: %w", err)
	}
	backendReleaseDeploymentID, err := s.patchAndStartManagedApplicationDomains(backend.UUID, managedFrontDoorBackendOrigin)
	if err != nil {
		return "", fmt.Errorf("releasing public origin from backend: %w", err)
	}
	return fmt.Sprintf("application_uuid: %s\naction: %s\nfront_door_backend_deployment_id: %s\nbackend_release_deployment_id: %s\nfront_door_origin: %s\nbackend_origin: %s\nfront_door_commit: %s\nnext_action: %s\n", app.UUID, frontDoorActionResumeCutoverBackend, deploymentID, backendReleaseDeploymentID, currentFrontOrigin, managedFrontDoorBackendOrigin, sha, frontDoorActionResumeCutoverPublic), nil
}

func (s *PlatformCapability) executeManagedFrontDoorCutoverPublicReady(app, _ platformApplication, request PlatformFrontDoorRequest, sha string) (string, error) {
	currentFrontOrigin := strings.TrimSpace(app.domain())
	if err := s.probeManagedOrigin(currentFrontOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		return "", fmt.Errorf("front door did not retain the alternate backend: %w", err)
	}
	publicDeploymentID, err := s.patchAndDeployManagedFrontDoorDomain(app.UUID, managedFrontDoorPublicOrigin)
	if err != nil {
		compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, currentFrontOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
		return "", managedTransitionError("assigning public origin to front door", err, compensationErr)
	}
	if err := s.probeManagedOrigin(managedFrontDoorPublicOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, currentFrontOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
		return "", managedTransitionError("public front door did not become ready", err, compensationErr)
	}
	return fmt.Sprintf("application_uuid: %s\naction: %s\npublic_domain_deployment_id: %s\npublic_origin: %s\nbackend_origin: %s\nfront_door_commit: %s\nrollback_request_domain: %s\nrollback_request_backend: %s\n", app.UUID, frontDoorActionResumeCutoverPublic, publicDeploymentID, managedFrontDoorPublicOrigin, managedFrontDoorBackendOrigin, sha, managedFrontDoorTemporaryOrigin, managedFrontDoorPublicOrigin), nil
}

func (s *PlatformCapability) executeManagedFrontDoorRollback(app, backend platformApplication, request PlatformFrontDoorRequest, sha string) (string, error) {
	temporaryDeploymentID, err := s.patchAndDeployManagedFrontDoorDomain(app.UUID, managedFrontDoorTemporaryOrigin)
	if err != nil {
		compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, managedFrontDoorPublicOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
		return "", managedTransitionError("moving front door to temporary origin", err, compensationErr)
	}
	if err := s.probeManagedOrigin(managedFrontDoorTemporaryOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		compensationErr := s.restoreManagedFrontDoorDomain(app.UUID, managedFrontDoorPublicOrigin, sha, request.ExpectedProtocol, request.ExpectedCatalogHash)
		return "", managedTransitionError("temporary front door did not become ready during rollback", err, compensationErr)
	}
	publicBackendDeploymentID, err := s.patchAndDeployManagedApplicationDomains(backend.UUID, managedFrontDoorBackendOrigin+","+managedFrontDoorPublicOrigin)
	if err != nil {
		return "", fmt.Errorf("restoring public backend origin: %w", err)
	}
	if err := s.probeManagedOrigin(managedFrontDoorPublicOrigin, false, "", request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		return "", fmt.Errorf("restored public backend origin did not become ready: %w", err)
	}
	deploymentID, err := s.configureAndDeployManagedFrontDoor(app.UUID, request)
	if err != nil {
		return "", err
	}
	if err := s.waitManagedDeployment(deploymentID); err != nil {
		return "", err
	}
	if err := s.probeManagedOrigin(managedFrontDoorTemporaryOrigin, true, sha, request.ExpectedProtocol, request.ExpectedCatalogHash); err != nil {
		return "", fmt.Errorf("rolled-back front door did not accept the public backend: %w", err)
	}
	backendCleanupDeploymentID, err := s.patchAndDeployManagedApplicationDomains(backend.UUID, managedFrontDoorPublicOrigin)
	if err != nil {
		return "", fmt.Errorf("removing alternate backend origin after rollback: %w", err)
	}
	_ = publicBackendDeploymentID
	_ = backendCleanupDeploymentID
	return fmt.Sprintf("application_uuid: %s\naction: %s\ntemporary_domain_deployment_id: %s\nbackend_switch_deployment_id: %s\nfront_door_origin: %s\nbackend_origin: %s\nfront_door_commit: %s\n", app.UUID, frontDoorActionRollback, temporaryDeploymentID, deploymentID, managedFrontDoorTemporaryOrigin, managedFrontDoorPublicOrigin, sha), nil
}

func (s *PlatformCapability) configureAndDeployManagedFrontDoor(appID string, request PlatformFrontDoorRequest) (string, error) {
	vars := map[string]string{
		"MCP_FRONT_DOOR_BACKEND_URL":           request.BackendURL,
		"MCP_FRONT_DOOR_EXPECTED_PROTOCOL":     request.ExpectedProtocol,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH": request.ExpectedCatalogHash,
	}
	keys := []string{"MCP_FRONT_DOOR_BACKEND_URL", "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH", "MCP_FRONT_DOOR_EXPECTED_PROTOCOL"}
	if _, err := s.coolify.setEnvironmentVariables(context.Background(), appID, vars, keys); err != nil {
		return "", fmt.Errorf("configuring managed front-door environment: %w", err)
	}
	status, body, err := s.coolify.deploy(context.Background(), appID, false)
	if err != nil {
		return "", fmt.Errorf("front-door deployment request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("front-door deployment -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	deployment := decodePlatformDeployResponse(body)
	if deployment.DeploymentUUID == "" {
		return "", errors.New("front-door deployment returned no deployment id")
	}
	return deployment.DeploymentUUID, nil
}

func (s *PlatformCapability) patchManagedApplicationDomains(appID, domains string) error {
	status, body, err := s.coolify.request(context.Background(), http.MethodPatch, "/api/v1/applications/"+url.PathEscape(appID), map[string]any{"domains": domains})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("domain update -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	return nil
}

func (s *PlatformCapability) patchAndDeployManagedFrontDoorDomain(appID, domain string) (string, error) {
	if err := s.patchManagedApplicationDomains(appID, domain); err != nil {
		return "", err
	}
	return s.deployAndWaitManagedApplication(appID)
}

func (s *PlatformCapability) patchAndDeployManagedApplicationDomains(appID, domains string) (string, error) {
	if err := s.patchManagedApplicationDomains(appID, domains); err != nil {
		return "", err
	}
	return s.deployAndWaitManagedApplication(appID)
}

func (s *PlatformCapability) patchAndStartManagedApplicationDomains(appID, domains string) (string, error) {
	if err := s.patchManagedApplicationDomains(appID, domains); err != nil {
		return "", err
	}
	return s.startManagedApplicationDeployment(appID)
}

func (s *PlatformCapability) deployAndWaitManagedApplication(appID string) (string, error) {
	deploymentID, err := s.startManagedApplicationDeployment(appID)
	if err != nil {
		return "", err
	}
	if err := s.waitManagedDeployment(deploymentID); err != nil {
		return deploymentID, err
	}
	return deploymentID, nil
}

func (s *PlatformCapability) startManagedApplicationDeployment(appID string) (string, error) {
	status, body, err := s.coolify.deploy(context.Background(), appID, false)
	if err != nil {
		return "", fmt.Errorf("managed application deployment request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("managed application deployment -> HTTP %d: %s", status, s.coolifySafe(body))
	}
	deployment := decodePlatformDeployResponse(body)
	if deployment.DeploymentUUID == "" {
		return "", errors.New("managed application deployment returned no deployment id")
	}
	return deployment.DeploymentUUID, nil
}

func (s *PlatformCapability) restoreManagedFrontDoorDomain(appID, domain, expectedCommit, expectedProtocol, expectedHash string) error {
	if _, err := s.patchAndDeployManagedFrontDoorDomain(appID, domain); err != nil {
		return err
	}
	return s.probeManagedOrigin(domain, true, expectedCommit, expectedProtocol, expectedHash)
}

func managedTransitionError(message string, cause, compensationErr error) error {
	if compensationErr == nil {
		return fmt.Errorf("%s: %w", message, cause)
	}
	return fmt.Errorf("%s: %w; compensation failed: %v", message, cause, compensationErr)
}

func (s *PlatformCapability) waitManagedDeployment(deploymentID string) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		status, body, err := s.coolify.request(context.Background(), http.MethodGet, "/api/v1/deployments/"+url.PathEscape(deploymentID), nil)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("reading front-door deployment -> HTTP %d: %s", status, s.coolifySafe(body))
		}
		var deployment platformDeployment
		if err := json.Unmarshal([]byte(body), &deployment); err != nil {
			return err
		}
		switch deployment.Status {
		case "finished":
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("front-door deployment ended in %s", deployment.Status)
		}
		s.managedFrontDoorSleep(2 * time.Second)
	}
	return errors.New("front-door deployment did not reach a terminal state")
}

func (s *PlatformCapability) probeManagedOrigin(origin string, frontDoor bool, expectedCommit, expectedProtocol, expectedHash string) error {
	if s.managedFrontDoorProbe != nil {
		return s.managedFrontDoorProbe(context.Background(), origin, frontDoor, expectedCommit, expectedProtocol, expectedHash)
	}
	deadline := time.Now().Add(90 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := probeManagedOriginOnce(origin, frontDoor, expectedCommit, expectedProtocol, expectedHash); err == nil {
			return nil
		} else {
			lastErr = err
		}
		s.managedFrontDoorSleep(time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("origin probe timed out")
	}
	return lastErr
}

func probeManagedOriginOnce(origin string, frontDoor bool, expectedCommit, expectedProtocol, expectedHash string) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ForceAttemptHTTP2 = false
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	path := "/version"
	if frontDoor {
		path = "/front-door/version"
	}
	request, _ := http.NewRequest(http.MethodGet, strings.TrimRight(origin, "/")+path, nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" {
		return fmt.Errorf("origin returned HTTP %d or a redirect", response.StatusCode)
	}
	if frontDoor {
		var info managedFrontDoorInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return err
		}
		if info.Status != "ok" || info.Commit != expectedCommit || !info.BackendReady || info.Backend.Status != "ok" || info.Backend.ProtocolVersion != expectedProtocol || info.Backend.CatalogHash != expectedHash || info.Backend.ToolCount < 1 {
			return errors.New("front-door identity does not match the managed contract")
		}
		if response.Header.Get("X-MCP-Front-Door-Commit") != expectedCommit {
			return errors.New("front-door identity header mismatch")
		}
		return nil
	}
	var info managedRuntimeInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return err
	}
	if info.Status != "ok" || info.ProtocolVersion != expectedProtocol || info.CatalogHash != expectedHash || info.ToolCount < 1 {
		return errors.New("backend identity does not match the managed contract")
	}
	if response.Header.Get("X-MCP-Server-Commit") != info.Commit || response.Header.Get("X-MCP-Catalog-Hash") != info.CatalogHash {
		return errors.New("backend identity header mismatch")
	}
	return nil
}

func (s *PlatformCapability) managedFrontDoorSleep(duration time.Duration) {
	if s.managedFrontDoorSleepFn != nil {
		s.managedFrontDoorSleepFn(duration)
		return
	}
	time.Sleep(duration)
}

func managedFrontDoorEffect(action string) string {
	switch action {
	case frontDoorActionCreate:
		return "create exactly one managed front-door application, configure its fixed contract, and deploy it"
	case frontDoorActionReconcile:
		return "reconcile the existing managed front door without changing its topology, then deploy only when required"
	case frontDoorActionRenameTemporary:
		return "replace only the legacy sslip.io front-door domain with the fixed DuckDNS subdomain and roll back automatically if readiness fails"
	case frontDoorActionCutover:
		return "start the normal backend routing deployment for the alternate DuckDNS origin and return its deployment id before the backend replaces itself"
	case frontDoorActionResumeCutoverBackend:
		return "verify the alternate backend, switch and deploy the front door, then start the normal backend deployment that releases the public origin"
	case frontDoorActionResumeCutoverPublic:
		return "verify the temporary front door against the alternate backend, assign the public origin to the front door, and verify the final topology"
	case frontDoorActionRollback:
		return "move the front door to its fixed temporary DuckDNS origin, restore and verify the public backend origin, redeploy the front door against it, and remove the alternate backend origin"
	default:
		return "reject an unknown managed front-door action"
	}
}
