package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

type startupCode string

const (
	startupInitializing                     startupCode = "initializing"
	startupConfigurationInvalid             startupCode = "configuration_invalid"
	startupJournalOpenFailed                startupCode = "journal_open_failed"
	startupCoolifyClientInvalid             startupCode = "coolify_client_invalid"
	startupTopologyValidationFailed         startupCode = "topology_validation_failed"
	startupTopologyFrontApplicationFailed   startupCode = "topology_front_application_failed"
	startupTopologyBackendApplicationFailed startupCode = "topology_backend_application_failed"
	startupTopologyIdentityInvalid          startupCode = "topology_identity_invalid"
	startupTopologyFrontBackendFailed       startupCode = "topology_front_backend_failed"
	startupTopologyContractInvalid          startupCode = "topology_contract_invalid"
	startupDurableStateFailed               startupCode = "durable_state_failed"
	startupStatusPublishFailed              startupCode = "status_publish_failed"
)

type coordinatorRuntimeState struct {
	mu      sync.RWMutex
	ready   bool
	code    startupCode
	journal *frontdoorcoordinator.Journal
}

type coordinatorStatusResponse struct {
	Ready   bool                         `json:"ready"`
	Code    startupCode                  `json:"code,omitempty"`
	Journal *frontdoorcoordinator.Status `json:"journal,omitempty"`
}

func newCoordinatorRuntimeState() *coordinatorRuntimeState {
	return &coordinatorRuntimeState{code: startupInitializing}
}

func (s *coordinatorRuntimeState) setJournal(journal *frontdoorcoordinator.Journal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journal = journal
}

func (s *coordinatorRuntimeState) setReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.code = ""
}

func (s *coordinatorRuntimeState) setFailure(code startupCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	s.code = code
}

func (s *coordinatorRuntimeState) snapshot() coordinatorStatusResponse {
	s.mu.RLock()
	ready, code, journal := s.ready, s.code, s.journal
	s.mu.RUnlock()
	response := coordinatorStatusResponse{Ready: ready, Code: code}
	if journal == nil {
		return response
	}
	status, err := journal.Read()
	if err != nil {
		response.Ready = false
		response.Code = startupJournalOpenFailed
		return response
	}
	status.RequestID = ""
	response.Journal = &status
	return response
}

func coordinatorHTTPHandler(state *coordinatorRuntimeState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok mcp-front-door-coordinator\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		response := state.snapshot()
		w.Header().Set("Content-Type", "application/json")
		if !response.Ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		response := state.snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	return mux
}

type coordinatorDependencies struct {
	openJournal func(string) (*frontdoorcoordinator.Journal, error)
	newPlatform func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error)
}

func defaultCoordinatorDependencies() coordinatorDependencies {
	return coordinatorDependencies{
		openJournal: frontdoorcoordinator.OpenJournal,
		newPlatform: func(config frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) {
			return frontdoorcoordinator.NewClient(config)
		},
	}
}

func coordinatorListenAddress(getenv func(string) string) string {
	raw := strings.TrimSpace(getenv(listenAddrEnv))
	if raw == "" {
		return defaultListenAddr
	}
	if _, _, err := net.SplitHostPort(raw); err != nil {
		return defaultListenAddr
	}
	return raw
}

func runCoordinator(ctx context.Context, getenv func(string) string, dependencies coordinatorDependencies) error {
	listener, err := net.Listen("tcp", coordinatorListenAddress(getenv))
	if err != nil {
		return errors.New("coordinator listener failed")
	}
	defer listener.Close()
	return serveCoordinator(ctx, listener, getenv, dependencies)
}

func serveCoordinator(ctx context.Context, listener net.Listener, getenv func(string) string, dependencies coordinatorDependencies) error {
	state := newCoordinatorRuntimeState()
	server := &http.Server{Handler: coordinatorHTTPHandler(state), ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- errors.New("coordinator HTTP server failed")
		}
	}()

	config, journal, platform, code := initializeCoordinator(ctx, getenv, dependencies)
	if journal != nil {
		state.setJournal(journal)
	}
	if code != "" {
		state.setFailure(code)
		log.Printf("front-door coordinator initialization failed: code=%s", code)
	} else {
		state.setReady()
		if config.Target != frontdoorcoordinator.TargetIdle {
			go runCoordinatorTransition(ctx, state, config, journal, platform)
		}
	}

	var result error
	select {
	case <-ctx.Done():
	case result = <-serverErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && result == nil {
		result = errors.New("coordinator HTTP shutdown failed")
	}
	return result
}

func topologyStartupCode(err error) startupCode {
	switch {
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication):
		return startupTopologyFrontApplicationFailed
	case errors.Is(err, frontdoorcoordinator.ErrTopologyBackendApplication):
		return startupTopologyBackendApplicationFailed
	case errors.Is(err, frontdoorcoordinator.ErrTopologyManagedIdentity):
		return startupTopologyIdentityInvalid
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontBackend):
		return startupTopologyFrontBackendFailed
	default:
		return startupTopologyValidationFailed
	}
}

func initializeCoordinator(ctx context.Context, getenv func(string) string, dependencies coordinatorDependencies) (runtimeConfig, *frontdoorcoordinator.Journal, frontdoorcoordinator.Platform, startupCode) {
	config, err := loadConfig(getenv)
	if err != nil {
		return runtimeConfig{}, nil, nil, startupConfigurationInvalid
	}
	if dependencies.openJournal == nil || dependencies.newPlatform == nil {
		return runtimeConfig{}, nil, nil, startupConfigurationInvalid
	}
	journal, err := dependencies.openJournal(config.StateRoot)
	if err != nil {
		return runtimeConfig{}, nil, nil, startupJournalOpenFailed
	}
	if _, err := journal.Read(); err != nil {
		return runtimeConfig{}, journal, nil, startupJournalOpenFailed
	}
	platform, err := dependencies.newPlatform(config.ClientConfig)
	if err != nil {
		return runtimeConfig{}, journal, nil, startupCoolifyClientInvalid
	}
	topology, err := platform.Topology(ctx)
	if err != nil {
		return runtimeConfig{}, journal, platform, topologyStartupCode(err)
	}
	if frontdoorcoordinator.ValidateTopology(config.Target, topology) != nil {
		return runtimeConfig{}, journal, platform, startupTopologyContractInvalid
	}
	return config, journal, platform, ""
}

func runCoordinatorTransition(ctx context.Context, state *coordinatorRuntimeState, config runtimeConfig, journal *frontdoorcoordinator.Journal, platform frontdoorcoordinator.Platform) {
	runner := frontdoorcoordinator.Runner{Platform: platform, Journal: journal, RequestID: config.RequestID}
	status, err := runner.Run(ctx, config.Target)
	if err == nil {
		log.Printf("front-door transition terminal: target=%s state=%s revision=%d", status.Target, status.State, status.Revision)
		return
	}
	log.Printf("front-door transition failed: target=%s state=%s phase=%s reason=%s", status.Target, status.State, status.Phase, status.Reason)
	if errors.Is(err, frontdoorcoordinator.ErrStatusPublish) {
		state.setFailure(startupStatusPublishFailed)
	}
	if errors.Is(err, frontdoorcoordinator.ErrDurableState) {
		state.setFailure(startupDurableStateFailed)
	}
}
