package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/frontdoor"
)

const (
	backendURLEnv        = "MCP_FRONT_DOOR_BACKEND_URL"
	expectedProtocolEnv  = "MCP_FRONT_DOOR_EXPECTED_PROTOCOL"
	expectedCatalogEnv   = "MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH"
	transitionCatalogEnv = "MCP_FRONT_DOOR_TRANSITION_CATALOG_HASH"
	listenAddrEnv        = "MCP_FRONT_DOOR_ADDR"
	probeIntervalEnv     = "MCP_FRONT_DOOR_PROBE_INTERVAL"
	probeTimeoutEnv      = "MCP_FRONT_DOOR_PROBE_TIMEOUT"
	admissionTimeoutEnv  = "MCP_FRONT_DOOR_ADMISSION_TIMEOUT"
	commitOverrideEnv    = "MCP_DEVBOX_COMMIT"
	commitSourceEnv      = "SOURCE_COMMIT"
)

func main() {
	config, addr, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, config, addr); err != nil {
		log.Fatal(err)
	}
}

func loadConfig(getenv func(string) string) (frontdoor.Config, string, error) {
	backendURL := strings.TrimSpace(getenv(backendURLEnv))
	protocol := strings.TrimSpace(getenv(expectedProtocolEnv))
	catalog := strings.TrimSpace(getenv(expectedCatalogEnv))
	if backendURL == "" || protocol == "" || catalog == "" {
		return frontdoor.Config{}, "", fmt.Errorf("%s, %s and %s are required", backendURLEnv, expectedProtocolEnv, expectedCatalogEnv)
	}
	addr := strings.TrimSpace(getenv(listenAddrEnv))
	if addr == "" {
		addr = "0.0.0.0:8765"
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return frontdoor.Config{}, "", fmt.Errorf("%s is invalid", listenAddrEnv)
	}
	probeInterval, err := parseDuration(getenv(probeIntervalEnv), time.Second, probeIntervalEnv)
	if err != nil {
		return frontdoor.Config{}, "", err
	}
	probeTimeout, err := parseDuration(getenv(probeTimeoutEnv), 3*time.Second, probeTimeoutEnv)
	if err != nil {
		return frontdoor.Config{}, "", err
	}
	admissionTimeout, err := parseDuration(getenv(admissionTimeoutEnv), 45*time.Second, admissionTimeoutEnv)
	if err != nil {
		return frontdoor.Config{}, "", err
	}
	config := frontdoor.Config{
		BackendURL: backendURL, ExpectedProtocol: protocol, ExpectedCatalogHash: catalog,
		FrontDoorCommit: resolveFrontDoorCommit(buildinfo.Commit, getenv), ProbeInterval: probeInterval, ProbeTimeout: probeTimeout, AdmissionTimeout: admissionTimeout,
	}
	if transition := strings.TrimSpace(getenv(transitionCatalogEnv)); transition != "" {
		config.TransitionCatalogHashes = []string{transition}
	}
	return config, addr, nil
}

func resolveFrontDoorCommit(linked string, getenv func(string) string) string {
	linked = strings.TrimSpace(linked)
	if linked != "" && linked != "unknown" {
		return linked
	}
	if getenv != nil {
		for _, name := range []string{commitOverrideEnv, commitSourceEnv} {
			if value := strings.TrimSpace(getenv(name)); value != "" {
				return value
			}
		}
	}
	return "unknown"
}

func parseDuration(raw string, fallback time.Duration, name string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func run(ctx context.Context, config frontdoor.Config, addr string) error {
	door, err := frontdoor.New(config)
	if err != nil {
		return err
	}
	go door.Run(ctx)
	server := &http.Server{
		Addr: addr, Handler: door.Handler(), ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 2 * time.Minute,
	}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("mcp front door starting: %s", door.String())
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}
