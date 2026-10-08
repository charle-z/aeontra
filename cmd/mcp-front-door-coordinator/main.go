package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const (
	coolifyURLEnv       = "COOLIFY_URL"
	coolifyTokenEnv     = "COOLIFY_API_TOKEN"
	coordinatorAppEnv   = "MCP_FRONT_DOOR_COORDINATOR_APP_UUID"
	frontAppEnv         = "MCP_FRONT_DOOR_APP_UUID"
	backendAppEnv       = "MCP_FRONT_DOOR_BACKEND_APP_UUID"
	frontCommitEnv      = "MCP_FRONT_DOOR_EXPECTED_COMMIT"
	backendCommitEnv    = "MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT"
	expectedProtocolEnv = "MCP_FRONT_DOOR_EXPECTED_PROTOCOL"
	expectedCatalogEnv  = "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"
	targetEnv           = "MCP_FRONT_DOOR_COORDINATOR_TARGET"
	requestIDEnv        = "MCP_FRONT_DOOR_COORDINATOR_REQUEST_ID"
	stateRootEnv        = "MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT"
	listenAddrEnv       = "MCP_FRONT_DOOR_COORDINATOR_ADDR"
	defaultStateRoot    = "/coordinator-state"
	defaultListenAddr   = "0.0.0.0:8766"
)

type runtimeConfig struct {
	ClientConfig frontdoorcoordinator.Config
	Target       frontdoorcoordinator.Target
	RequestID    string
	StateRoot    string
	ListenAddr   string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run := runCoordinator
	if catalogRolloutRequested(os.Getenv) {
		run = runCatalogCoordinator
	}
	if err := run(ctx, os.Getenv, defaultCoordinatorDependencies()); err != nil {
		log.Fatal("front-door coordinator stopped")
	}
}

func loadConfig(getenv func(string) string) (runtimeConfig, error) {
	if getenv == nil {
		return runtimeConfig{}, errors.New("environment reader is required")
	}
	targetRaw := strings.TrimSpace(getenv(targetEnv))
	if targetRaw == "" {
		targetRaw = string(frontdoorcoordinator.TargetIdle)
	}
	target, err := frontdoorcoordinator.ParseTarget(targetRaw)
	if err != nil {
		return runtimeConfig{}, err
	}
	requestID := strings.TrimSpace(getenv(requestIDEnv))
	if target != frontdoorcoordinator.TargetIdle {
		if err := frontdoorcoordinator.ValidateRequestID(requestID); err != nil {
			return runtimeConfig{}, err
		}
	}
	stateRoot := strings.TrimSpace(getenv(stateRootEnv))
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	if stateRoot != defaultStateRoot {
		return runtimeConfig{}, fmt.Errorf("%s must remain %s", stateRootEnv, defaultStateRoot)
	}
	listenAddr := strings.TrimSpace(getenv(listenAddrEnv))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	if _, _, err := net.SplitHostPort(listenAddr); err != nil {
		return runtimeConfig{}, fmt.Errorf("%s must be a host:port", listenAddrEnv)
	}
	return runtimeConfig{
		ClientConfig: frontdoorcoordinator.Config{
			CoolifyURL:            getenv(coolifyURLEnv),
			CoolifyToken:          getenv(coolifyTokenEnv),
			CoordinatorAppID:      strings.TrimSpace(getenv(coordinatorAppEnv)),
			FrontAppID:            strings.TrimSpace(getenv(frontAppEnv)),
			BackendAppID:          strings.TrimSpace(getenv(backendAppEnv)),
			ExpectedFrontCommit:   strings.TrimSpace(getenv(frontCommitEnv)),
			ExpectedBackendCommit: strings.TrimSpace(getenv(backendCommitEnv)),
			ExpectedProtocol:      strings.TrimSpace(getenv(expectedProtocolEnv)),
			ExpectedCatalogHash:   strings.TrimSpace(getenv(expectedCatalogEnv)),
		},
		Target: target, RequestID: requestID, StateRoot: stateRoot, ListenAddr: listenAddr,
	}, nil
}
