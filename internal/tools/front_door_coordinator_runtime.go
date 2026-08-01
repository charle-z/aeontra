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

	coordinatorCoolifyURL, err := s.managedFrontDoorCoordinatorCoolifyURL()
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	entries, err := s.coolify.listEnvironmentVariables(context.Background(), app.UUID)
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	environment := map[string]coolifyEnvironmentVariable{}
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
		if _, exists := environment[entry.Key]; exists {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed coordinator environment key %s is ambiguous", entry.Key)
		}
		if !entry.IsLiteral || !entry.IsRuntime || entry.IsBuildtime {
			return managedFrontDoorCoordinatorIdentity{}, fmt.Errorf("managed coordinator environment key %s has invalid metadata", entry.Key)
		}
		environment[entry.Key] = entry
	}
	resolve := func(key string, candidates ...string) (string, error) {
		entry, ok := environment[key]
		if !ok {
			return "", fmt.Errorf("managed coordinator environment key %s is absent", key)
		}
		value, err := frontdoorcoordinator.ManagedEnvironmentValue(entry.Comment, s.coolify.token, key, candidates...)
		if err != nil {
			return "", fmt.Errorf("managed coordinator environment key %s does not match the fixed contract: %w", key, err)
		}
		return value, nil
	}
	if _, err := resolve("COOLIFY_API_TOKEN", s.coolify.token); err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	expected := map[string]string{
		"COOLIFY_URL":                            coordinatorCoolifyURL,
		"MCP_FRONT_DOOR_COORDINATOR_APP_UUID":    app.UUID,
		"MCP_FRONT_DOOR_APP_UUID":                front.UUID,
		"MCP_FRONT_DOOR_BACKEND_APP_UUID":        backend.UUID,
		"MCP_FRONT_DOOR_EXPECTED_COMMIT":         frontSHA,
		"MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT": mainSHA,
		"MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT":  managedFrontDoorCoordinatorStateMount,
		"MCP_FRONT_DOOR_COORDINATOR_ADDR":        "0.0.0.0:" + managedFrontDoorCoordinatorPort,
	}
	for key, want := range expected {
		if _, err := resolve(key, want); err != nil {
			return managedFrontDoorCoordinatorIdentity{}, err
		}
	}
	protocol, err := resolve("MCP_FRONT_DOOR_EXPECTED_PROTOCOL")
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	catalogHash, err := resolve("MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH")
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	if !frontDoorProtocolPattern.MatchString(protocol) || !frontDoorCatalogPattern.MatchString(catalogHash) {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator compatibility environment is invalid")
	}
	targetValue, err := resolve("MCP_FRONT_DOOR_COORDINATOR_TARGET")
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	target, err := frontdoorcoordinator.ParseTarget(targetValue)
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	published, present, err := frontdoorcoordinator.DecodePublishedStatus(app.Description)
	if err != nil {
		return managedFrontDoorCoordinatorIdentity{}, err
	}
	expectedRequestID := ""
	if present {
		if published.Target != target {
			return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator target does not match the durable journal")
		}
		expectedRequestID = published.RequestID
	} else if target != frontdoorcoordinator.TargetIdle {
		return managedFrontDoorCoordinatorIdentity{}, errors.New("managed coordinator non-idle target has no durable published state")
	}
	if expectedRequestID != "" {
		if err := frontdoorcoordinator.ValidateRequestID(expectedRequestID); err != nil {
			return managedFrontDoorCoordinatorIdentity{}, err
		}
		if _, err := resolve("MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID", expectedRequestID); err != nil {
			return managedFrontDoorCoordinatorIdentity{}, err
		}
	} else if _, exists := environment["MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID"]; exists {
		if _, err := resolve("MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID", ""); err != nil {
			return managedFrontDoorCoordinatorIdentity{}, err
		}
	}
	return managedFrontDoorCoordinatorIdentity{MainCommit: mainSHA, FrontCommit: frontSHA, Protocol: protocol, CatalogHash: catalogHash}, nil
}
