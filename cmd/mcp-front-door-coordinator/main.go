package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	config, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	journal, err := frontdoorcoordinator.OpenJournal(config.StateRoot)
	if err != nil {
		log.Fatal(err)
	}
	client, err := frontdoorcoordinator.NewClient(config.ClientConfig)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := journal.Read(); err != nil {
			http.Error(w, "journal unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok mcp-front-door-coordinator\n"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		status, err := journal.Read()
		if err != nil {
			http.Error(w, "journal unavailable", http.StatusServiceUnavailable)
			return
		}
		status.RequestID = ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: config.ListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("coordinator HTTP server failed: %v", err)
			stop()
		}
	}()

	fatalTransition := make(chan error, 1)
	if config.Target != frontdoorcoordinator.TargetIdle {
		go func() {
			runner := frontdoorcoordinator.Runner{Platform: client, Journal: journal, RequestID: config.RequestID}
			status, runErr := runner.Run(ctx, config.Target)
			if runErr != nil {
				log.Printf("front-door transition failed: target=%s state=%s phase=%s reason=%s", status.Target, status.State, status.Phase, status.Reason)
				if errors.Is(runErr, frontdoorcoordinator.ErrStatusPublish) || errors.Is(runErr, frontdoorcoordinator.ErrDurableState) {
					fatalTransition <- runErr
				}
				return
			}
			log.Printf("front-door transition terminal: target=%s state=%s revision=%d", status.Target, status.State, status.Revision)
		}()
	}

	var fatalErr error
	select {
	case <-ctx.Done():
	case fatalErr = <-fatalTransition:
		stop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("coordinator HTTP shutdown failed: %v", err)
	}
	if fatalErr != nil {
		log.Fatalf("front-door coordinator cannot maintain durable state: %v", fatalErr)
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
	listenAddr := strings.TrimSpace(getenv(listenAddrEnv))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	if !strings.Contains(listenAddr, ":") {
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
