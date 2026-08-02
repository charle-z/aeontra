package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const (
	frontDoorExpectedCatalogKey   = "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"
	frontDoorTransitionCatalogKey = "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH"
)

type managedFrontDoorCatalogPlan struct {
	Primary    string
	Transition string
	RemoveUUID string
	Changed    bool
}

func planManagedFrontDoorCatalogTransition(entries []coolifyEnvironmentVariable, token, requested string) (managedFrontDoorCatalogPlan, error) {
	requested = strings.TrimSpace(requested)
	if !frontDoorCatalogPattern.MatchString(requested) {
		return managedFrontDoorCatalogPlan{}, errors.New("requested front-door catalog hash is invalid")
	}
	byKey := map[string][]coolifyEnvironmentVariable{}
	for _, entry := range entries {
		if entry.IsPreview {
			continue
		}
		switch entry.Key {
		case frontDoorExpectedCatalogKey, frontDoorTransitionCatalogKey:
			byKey[entry.Key] = append(byKey[entry.Key], entry)
		}
	}
	if len(byKey[frontDoorExpectedCatalogKey]) > 1 || len(byKey[frontDoorTransitionCatalogKey]) > 1 {
		return managedFrontDoorCatalogPlan{}, errors.New("managed front-door catalog environment is ambiguous")
	}
	if len(byKey[frontDoorExpectedCatalogKey]) == 0 {
		if len(byKey[frontDoorTransitionCatalogKey]) != 0 {
			return managedFrontDoorCatalogPlan{}, errors.New("managed front-door transition catalog exists without a primary catalog")
		}
		return managedFrontDoorCatalogPlan{Primary: requested, Changed: true}, nil
	}
	primary, err := authenticatedManagedCatalog(byKey[frontDoorExpectedCatalogKey][0], token)
	if err != nil {
		return managedFrontDoorCatalogPlan{}, fmt.Errorf("managed front-door primary catalog is invalid: %w", err)
	}
	transition := ""
	transitionUUID := ""
	if len(byKey[frontDoorTransitionCatalogKey]) == 1 {
		entry := byKey[frontDoorTransitionCatalogKey][0]
		transition, err = authenticatedManagedCatalog(entry, token)
		if err != nil {
			return managedFrontDoorCatalogPlan{}, fmt.Errorf("managed front-door transition catalog is invalid: %w", err)
		}
		transitionUUID = entry.UUID
		if transition == primary {
			return managedFrontDoorCatalogPlan{}, errors.New("managed front-door catalog hashes are duplicated")
		}
	}
	if primary == requested {
		if transition == "" {
			return managedFrontDoorCatalogPlan{Primary: primary}, nil
		}
		if strings.TrimSpace(transitionUUID) == "" {
			return managedFrontDoorCatalogPlan{}, errors.New("managed front-door transition catalog identity is invalid")
		}
		return managedFrontDoorCatalogPlan{Primary: primary, RemoveUUID: transitionUUID, Changed: true}, nil
	}
	if transition != "" && transition != requested {
		return managedFrontDoorCatalogPlan{}, errors.New("managed front-door rollout would require a third catalog")
	}
	return managedFrontDoorCatalogPlan{Primary: requested, Transition: primary, Changed: true}, nil
}

func authenticatedManagedCatalog(entry coolifyEnvironmentVariable, token string) (string, error) {
	value := strings.TrimSpace(entry.Value)
	if value != entry.Value || !frontDoorCatalogPattern.MatchString(value) {
		return "", errors.New("catalog hash is malformed")
	}
	if !entry.IsLiteral || !entry.IsRuntime || entry.IsBuildtime {
		return "", errors.New("environment metadata is outside the managed contract")
	}
	resolved, err := frontdoorcoordinator.ManagedEnvironmentValue(entry.Comment, token, entry.Key, value)
	if err != nil || resolved != value {
		return "", errors.New("environment authentication failed")
	}
	return value, nil
}
