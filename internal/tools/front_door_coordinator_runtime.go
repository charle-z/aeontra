package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

type managedFrontDoorCoordinatorIdentity struct {
	MainCommit  string
	FrontCommit string
	Protocol    string
	CatalogHash string
}

func (s *PlatformCapability) verifyManagedFrontDoorCoordinatorRuntime(app, front, backend platformApplication) (managedFrontDoorCoordinatorIdentity, error) {
	if err := s.validateManagedFrontDoorCoordinatorApp(app); err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	mainSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorCoordinatorBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(mainSHA) {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("main branch returned an invalid commit")
	}
	frontSHA, err := s.github.branchSHA(context.Background(), "mcp-devbox", managedFrontDoorBranch)
	if err != nil || !frontDoorCommitPattern.MatchString(frontSHA) {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("stable front-door branch returned an invalid commit")
	}
	for name, current := range map[string]platformApplication{
		"coordinator": app,
		"front door":  front,
		"backend":     backend,
	} {
		if current.Status != "running:healthy" || current.DeploymentStatus != "finished" {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed %s application is not running healthy on a finished deployment", name)
		}
	}
	if app.commit() != mainSHA || backend.commit() != mainSHA || front.commit() != frontSHA {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed front-door deployment commits do not match the approved branches")
	}

	entries, err := s.coolify.listEnvironmentVariables(context.Background(), app.UUID)
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	values := map[string]string{}
	allowed := map[string]bool{
		"COOLIFY_URL": true, "COOLIFY_API_TOKEN": true,
		"MCP_FRONT_DOOR_COORDINATOR_APP_UUID": true, "MCP_FRONT_DOOR_APP_UUID": true,
		"MCP_FRONT_DOOR_BACKEND_APP_UUID": true, "MCP_FRONT_DOOR_EXPECTED_COMMIT": true,
		"MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT": true, "MCP_FRONT_DOOR_EXPECTED_PROTOCOL": true,
		"MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH": true, "MCP_FRONT_DOOR_COORDINATOR_TARGET": true,
		"MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID": true, "MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT": true,
		"MCP_FRONT_DOOR_COORDINATOR_ADDR": true,
	}
	for _, entry := range entries {
		if entry.IsPreview {
			continue
		}
		if (strings.HasPrefix(entry.Key, "MCP_FRONT_DOOR_") || entry.Key == "COOLIFY_URL" || entry.Key == "COOLIFY_API_TOKEN") && !allowed[entry.Key] {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed coordinator has unexpected environment key %s", entry.Key)
		}
		if !allowed[entry.Key] {
			continue
		}
		if _, exists := values[entry.Key]; exists {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed coordinator environment key %s is ambiguous", entry.Key)
		}
		values[entry.Key] = strings.TrimSpace(entry.Value)
	}
	if !managedCoordinatorSecretMatches(values["COOLIFY_API_TOKEN"], s.coolify.token) {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator Coolify token is absent or invalid")
	}
	expected := map[string]string{
		"COOLIFY_URL":                            s.coolify.baseURL,
		"MCP_FRONT_DOOR_COORDINATOR_APP_UUID":    app.UUID,
		"MCP_FRONT_DOOR_APP_UUID":                front.UUID,
		"MCP_FRONT_DOOR_BACKEND_APP_UUID":        backend.UUID,
		"MCP_FRONT_DOOR_EXPECTED_COMMIT":         frontSHA,
		"MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT": mainSHA,
		"MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT":  managedFrontDoorCoordinatorStateMount,
		"MCP_FRONT_DOOR_COORDINATOR_ADDR":        "0.0.0.0:" + managedFrontDoorCoordinatorPort,
	}
	for key, want := range expected {
		if values[key] != want {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed coordinator environment key %s does not match the fixed contract", key)
		}
	}
	protocol := values["MCP_FRONT_DOOR_EXPECTED_PROTOCOL"]
	catalogHash := values["MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"]
	if !frontDoorProtocolPattern.MatchString(protocol) || !frontDoorCatalogPattern.MatchString(catalogHash) {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator compatibility environment is invalid")
	}
	target, err := frontdoorcoordinator.ParseTarget(values["MCP_FRONT_DOOR_COORDINATOR_TARGET"])
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	requestID := values["MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID"]
	if requestID != "" {
		if err := frontdoorcoordinator.ValidateRequestID(requestID); err != nil {
			return managedFrontDoorCoordinatorIdentity{}, err
		}
	}
	if target != frontdoorcoordinator.TargetIdle && requestID == "" {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator non-idle target has no durable request id")
	}
	return managedFrontDoorCoordinatorIdentity{MainCommit: mainSHA, FrontCommit: frontSHA, Protocol: protocol, CatalogHash: catalogHash}, nil
}

func managedCoordinatorSecretMatches(got, want string) bool {
	if got == want && got != "" {
		return true
	}
	return len(got) >= 4 && strings.Trim(got, "*") == ""
}
