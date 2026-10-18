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
	startupInitializing                      startupCode = "initializing"
	startupConfigurationInvalid              startupCode = "configuration_invalid"
	startupJournalOpenFailed                 startupCode = "journal_open_failed"
	startupCoolifyClientInvalid              startupCode = "coolify_client_invalid"
	startupTopologyValidationFailed          startupCode = "topology_validation_failed"
	startupTopologyFrontApplicationFailed    startupCode = "topology_front_application_failed"
	startupTopologyFrontApplicationBuild     startupCode = "topology_front_application_request_build_failed"
	startupTopologyFrontApplicationTransport startupCode = "topology_front_application_transport_failed"
	startupTopologyFrontTransportTarget      startupCode = "topology_front_application_transport_target_failed"
	startupTopologyFrontTransportResolve     startupCode = "topology_front_application_transport_resolution_failed"
	startupTopologyFrontTransportAddress     startupCode = "topology_front_application_transport_address_policy_failed"
	startupTopologyFrontTransportRefused     startupCode = "topology_front_application_transport_connection_refused"
	startupTopologyFrontTransportTimeout     startupCode = "topology_front_application_transport_connection_timed_out"
	startupTopologyFrontTransportRoute       startupCode = "topology_front_application_transport_route_unavailable"
	startupTopologyFrontTransportConnect     startupCode = "topology_front_application_transport_connection_failed"
	startupTopologyFrontApplicationRead      startupCode = "topology_front_application_response_read_failed"
	startupTopologyFrontApplicationHTTP      startupCode = "topology_front_application_http_failed"
	startupTopologyFrontApplicationDecode    startupCode = "topology_front_application_decode_failed"
	startupTopologyFrontApplicationIdentity  startupCode = "topology_front_application_identity_failed"
	startupTopologyBackendApplicationFailed  startupCode = "topology_backend_application_failed"
	startupTopologyIdentityInvalid           startupCode = "topology_identity_invalid"
	startupTopologyFrontBackendFailed        startupCode = "topology_front_backend_failed"
	startupTopologyContractInvalid           startupCode = "topology_contract_invalid"
	startupDurableStateFailed                startupCode = "durable_state_failed"
	startupStatusPublishFailed               startupCode = "status_publish_failed"
	startupStatusPublishBuild                startupCode = "status_publish_request_build_failed"
	startupStatusPublishTransport            startupCode = "status_publish_transport_failed"
	startupStatusPublishTransportTarget      startupCode = "status_publish_transport_target_failed"
	startupStatusPublishTransportResolve     startupCode = "status_publish_transport_resolution_failed"
	startupStatusPublishTransportAddress     startupCode = "status_publish_transport_address_policy_failed"
	startupStatusPublishTransportRefused     startupCode = "status_publish_transport_connection_refused"
	startupStatusPublishTransportTimeout     startupCode = "status_publish_transport_connection_timed_out"
	startupStatusPublishTransportRoute       startupCode = "status_publish_transport_route_unavailable"
	startupStatusPublishTransportConnect     startupCode = "status_publish_transport_connection_failed"
	startupStatusPublishRead                 startupCode = "status_publish_response_read_failed"
	startupStatusPublishHTTP                 startupCode = "status_publish_http_failed"
	startupStatusPublishDecode               startupCode = "status_publish_response_decode_failed"
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

const defaultTransitionStartDelay = 75 * time.Second

type coordinatorDependencies struct {
	openJournal            func(string) (*frontdoorcoordinator.Journal, error)
	newPlatform            func(frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error)
	waitForTransitionStart func(context.Context) error
}

func defaultCoordinatorDependencies() coordinatorDependencies {
	return coordinatorDependencies{
		openJournal: frontdoorcoordinator.OpenJournal,
		newPlatform: func(config frontdoorcoordinator.Config) (frontdoorcoordinator.Platform, error) {
			return frontdoorcoordinator.NewClient(config)
		},
		waitForTransitionStart: func(ctx context.Context) error {
			timer := time.NewTimer(defaultTransitionStartDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
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
			go func() {
				if dependencies.waitForTransitionStart != nil {
					if err := dependencies.waitForTransitionStart(ctx); err != nil {
						return
					}
				}
				runCoordinatorTransition(ctx, state, config, journal, platform)
			}()
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
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyRequestBuild):
		return startupTopologyFrontApplicationBuild
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateTarget):
		return startupTopologyFrontTransportTarget
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateResolve):
		return startupTopologyFrontTransportResolve
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateAddress):
		return startupTopologyFrontTransportAddress
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateRefused):
		return startupTopologyFrontTransportRefused
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateTimeout):
		return startupTopologyFrontTransportTimeout
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateRoute):
		return startupTopologyFrontTransportRoute
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateConnect):
		return startupTopologyFrontTransportConnect
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyRequestTransport):
		return startupTopologyFrontApplicationTransport
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseRead):
		return startupTopologyFrontApplicationRead
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseHTTP):
		return startupTopologyFrontApplicationHTTP
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseDecode):
		return startupTopologyFrontApplicationDecode
	case errors.Is(err, frontdoorcoordinator.ErrTopologyFrontApplication) && errors.Is(err, frontdoorcoordinator.ErrCoolifyIdentity):
		return startupTopologyFrontApplicationIdentity
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

func statusPublishStartupCode(err error) startupCode {
	switch {
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyRequestBuild):
		return startupStatusPublishBuild
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateTarget):
		return startupStatusPublishTransportTarget
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateResolve):
		return startupStatusPublishTransportResolve
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateAddress):
		return startupStatusPublishTransportAddress
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateRefused):
		return startupStatusPublishTransportRefused
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateTimeout):
		return startupStatusPublishTransportTimeout
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateRoute):
		return startupStatusPublishTransportRoute
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyPrivateConnect):
		return startupStatusPublishTransportConnect
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyRequestTransport):
		return startupStatusPublishTransport
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseRead):
		return startupStatusPublishRead
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseHTTP):
		return startupStatusPublishHTTP
	case errors.Is(err, frontdoorcoordinator.ErrCoolifyResponseDecode):
		return startupStatusPublishDecode
	default:
		return startupStatusPublishFailed
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
	persisted, err := journal.Read()
	if err != nil {
		return runtimeConfig{}, journal, nil, startupJournalOpenFailed
	}
	config = resumeActiveCoordinatorRequest(config, persisted)
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

func resumeActiveCoordinatorRequest(config runtimeConfig, persisted frontdoorcoordinator.Status) runtimeConfig {
	switch persisted.State {
	case frontdoorcoordinator.StateQueued, frontdoorcoordinator.StateRunning, frontdoorcoordinator.StateCompensating:
		config.Target = persisted.Target
		config.RequestID = persisted.RequestID
	}
	return config
}

func runCoordinatorTransition(ctx context.Context, state *coordinatorRuntimeState, config runtimeConfig, journal *frontdoorcoordinator.Journal, platform frontdoorcoordinator.Platform) {
	runner := frontdoorcoordinator.Runner{Platform: platform, Journal: journal, RequestID: config.RequestID}
	status, err := runner.Run(ctx, config.Target)
	if err == nil {
		log.Printf("front-door transition terminal: target=%s state=%s revision=%d", status.Target, status.State, status.Revision)
		return
	}
	code := startupCode("")
	if errors.Is(err, frontdoorcoordinator.ErrStatusPublish) {
		code = statusPublishStartupCode(err)
		state.setFailure(code)
	}
	if errors.Is(err, frontdoorcoordinator.ErrDurableState) {
		code = startupDurableStateFailed
		state.setFailure(code)
	}
	log.Printf("front-door transition failed: code=%s target=%s state=%s phase=%s reason=%s", code, status.Target, status.State, status.Phase, status.Reason)
}
