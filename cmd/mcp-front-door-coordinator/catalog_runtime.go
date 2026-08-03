package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/catalogrollout"
	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

const (
	catalogRequestEnv  = "MCP_FRONT_DOOR_CATALOG_ROLLOUT_REQUEST"
	catalogMCPTokenEnv = "MCP_FRONT_DOOR_CATALOG_MCP_TOKEN"
)

type catalogRuntimeConfig struct {
	ClientConfig frontdoorcoordinator.Config
	Request      catalogrollout.Request
	MCPToken     string
	StateRoot    string
	ListenAddr   string
}

func catalogRolloutRequested(getenv func(string) string) bool {
	return getenv != nil && strings.TrimSpace(getenv(catalogRequestEnv)) != ""
}

func loadCatalogRuntimeConfig(getenv func(string) string) (catalogRuntimeConfig, error) {
	base, err := loadConfig(getenv)
	if err != nil {
		return catalogRuntimeConfig{}, err
	}
	if base.Target != frontdoorcoordinator.TargetIdle || base.RequestID != "" {
		return catalogRuntimeConfig{}, errors.New("catalog rollout cannot share a topology transition request")
	}
	raw := strings.TrimSpace(getenv(catalogRequestEnv))
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request catalogrollout.Request
	if err := decoder.Decode(&request); err != nil {
		return catalogRuntimeConfig{}, errors.New("catalog rollout request is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return catalogRuntimeConfig{}, errors.New("catalog rollout request has trailing data")
	}
	if err := request.Validate(); err != nil {
		return catalogRuntimeConfig{}, err
	}
	if base.ClientConfig.ExpectedBackendCommit != request.Previous.Commit ||
		base.ClientConfig.ExpectedProtocol != request.Previous.ProtocolVersion ||
		base.ClientConfig.ExpectedCatalogHash != request.Previous.CatalogHash {
		return catalogRuntimeConfig{}, errors.New("catalog rollout previous identity does not match the server-owned coordinator contract")
	}
	token := strings.TrimSpace(getenv(catalogMCPTokenEnv))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return catalogRuntimeConfig{}, errors.New("catalog rollout MCP token is invalid")
	}
	return catalogRuntimeConfig{
		ClientConfig: base.ClientConfig,
		Request:      request,
		MCPToken:     token,
		StateRoot:    filepath.Join(base.StateRoot, "catalog-rollout"),
		ListenAddr:   base.ListenAddr,
	}, nil
}

type catalogRuntimeState struct {
	mu      sync.RWMutex
	ready   bool
	code    startupCode
	journal *catalogrollout.Journal
}

type catalogStatusResponse struct {
	Ready   bool                   `json:"ready"`
	Code    startupCode            `json:"code,omitempty"`
	Journal *catalogrollout.Status `json:"journal,omitempty"`
}

func (s *catalogRuntimeState) snapshot() catalogStatusResponse {
	s.mu.RLock()
	ready, code, journal := s.ready, s.code, s.journal
	s.mu.RUnlock()
	response := catalogStatusResponse{Ready: ready, Code: code}
	if journal == nil {
		return response
	}
	status, err := journal.Read()
	if err != nil {
		response.Ready = false
		response.Code = startupJournalOpenFailed
		return response
	}
	status.Request.RequestID = ""
	response.Journal = &status
	return response
}

func catalogHTTPHandler(state *catalogRuntimeState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok mcp-front-door-coordinator\n"))
	})
	for _, path := range []string{"/readyz", "/status"} {
		path := path
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
			response := state.snapshot()
			w.Header().Set("Content-Type", "application/json")
			if path == "/readyz" && !response.Ready {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			_ = json.NewEncoder(w).Encode(response)
		})
	}
	return mux
}

func serveCatalogCoordinator(ctx context.Context, listener net.Listener, getenv func(string) string, dependencies coordinatorDependencies) error {
	state := &catalogRuntimeState{code: startupInitializing}
	server := &http.Server{Handler: catalogHTTPHandler(state), ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- errors.New("catalog coordinator HTTP server failed")
		}
	}()

	config, err := loadCatalogRuntimeConfig(getenv)
	if err != nil {
		state.code = startupConfigurationInvalid
	} else {
		journal, journalErr := catalogrollout.OpenJournal(config.StateRoot)
		if journalErr != nil {
			state.code = startupJournalOpenFailed
		} else {
			client, clientErr := frontdoorcoordinator.NewClient(config.ClientConfig)
			if clientErr != nil {
				state.code = startupCoolifyClientInvalid
			} else {
				platform, platformErr := frontdoorcoordinator.NewCatalogPlatform(client, config.MCPToken)
				if platformErr != nil {
					state.code = startupConfigurationInvalid
				} else {
					state.mu.Lock()
					state.journal = journal
					state.ready = true
					state.code = ""
					state.mu.Unlock()
					go func() {
						if dependencies.waitForTransitionStart != nil {
							if err := dependencies.waitForTransitionStart(ctx); err != nil {
								return
							}
						}
						status, runErr := (catalogrollout.Runner{Platform: platform, Journal: journal}).Run(ctx, config.Request)
						if runErr != nil {
							log.Printf("catalog rollout terminal: state=%s phase=%s reason=%s", status.State, status.Phase, status.Reason)
							return
						}
						log.Printf("catalog rollout terminal: state=%s phase=%s revision=%d", status.State, status.Phase, status.Revision)
					}()
				}
			}
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
		result = errors.New("catalog coordinator HTTP shutdown failed")
	}
	return result
}
