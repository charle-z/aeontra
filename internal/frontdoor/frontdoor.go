package frontdoor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultProbeInterval = time.Second
	defaultProbeTimeout  = 3 * time.Second
	maxVersionBody       = 64 << 10
)

var (
	protocolPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	catalogPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	commitPattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

// Config is immutable front-door configuration. The backend origin is operator-owned;
// callers cannot choose a target per request.
type Config struct {
	BackendURL          string
	ExpectedProtocol    string
	ExpectedCatalogHash string
	FrontDoorCommit     string
	ProbeInterval       time.Duration
	ProbeTimeout        time.Duration
	Client              *http.Client
}

type runtimeInfo struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at,omitempty"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

type snapshot struct {
	Ready     bool
	Info      runtimeInfo
	CheckedAt time.Time
	Reason    string
}

// FrontDoor is a stateless MCP-aware reverse proxy. It keeps accepted requests bound
// to the backend connection that accepted them while readiness changes only gate new
// requests.
type FrontDoor struct {
	backend             *url.URL
	expectedProtocol    string
	expectedCatalogHash string
	frontDoorCommit     string
	probeInterval       time.Duration
	probeTimeout        time.Duration
	probeClient         *http.Client
	proxy               *httputil.ReverseProxy
	state               atomic.Pointer[snapshot]
	activeRequests      atomic.Int64
}

func New(config Config) (*FrontDoor, error) {
	backend, err := validateBackendURL(config.BackendURL)
	if err != nil {
		return nil, err
	}
	if !protocolPattern.MatchString(config.ExpectedProtocol) {
		return nil, errors.New("front door expected protocol is invalid")
	}
	if !catalogPattern.MatchString(config.ExpectedCatalogHash) {
		return nil, errors.New("front door expected catalog hash is invalid")
	}
	if config.FrontDoorCommit != "" && config.FrontDoorCommit != "unknown" && !commitPattern.MatchString(config.FrontDoorCommit) {
		return nil, errors.New("front door commit is invalid")
	}
	if config.ProbeInterval <= 0 {
		config.ProbeInterval = defaultProbeInterval
	}
	if config.ProbeInterval < 250*time.Millisecond || config.ProbeInterval > time.Minute {
		return nil, errors.New("front door probe interval is invalid")
	}
	if config.ProbeTimeout <= 0 {
		config.ProbeTimeout = defaultProbeTimeout
	}
	if config.ProbeTimeout < 250*time.Millisecond || config.ProbeTimeout > 10*time.Second {
		return nil, errors.New("front door probe timeout is invalid")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport}
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.FlushInterval = -1

	front := &FrontDoor{
		backend: backend, expectedProtocol: config.ExpectedProtocol, expectedCatalogHash: config.ExpectedCatalogHash,
		frontDoorCommit: config.FrontDoorCommit, probeInterval: config.ProbeInterval, probeTimeout: config.ProbeTimeout,
		probeClient: client, proxy: proxy,
	}
	front.state.Store(&snapshot{Reason: "backend_not_probed"})
	proxy.ErrorHandler = front.proxyError
	proxy.ModifyResponse = front.validateResponse
	return front, nil
}

func validateBackendURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("front door backend URL is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("front door backend URL must be an origin without a path")
	}
	parsed.Path = ""
	switch parsed.Scheme {
	case "https":
	case "http":
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("front door plaintext backend must be loopback")
		}
	default:
		return nil, errors.New("front door backend URL scheme is invalid")
	}
	return parsed, nil
}

// Probe verifies readiness and the exact public MCP compatibility identity.
func (f *FrontDoor) Probe(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, f.probeTimeout)
	defer cancel()
	if err := f.probeReady(ctx); err != nil {
		f.storeUnavailable(err)
		return err
	}
	info, err := f.probeVersion(ctx)
	if err != nil {
		f.storeUnavailable(err)
		return err
	}
	if info.Status != "ok" || info.ProtocolVersion != f.expectedProtocol || info.CatalogHash != f.expectedCatalogHash ||
		!commitPattern.MatchString(info.Commit) || info.ToolCount < 1 {
		err = errors.New("backend compatibility identity does not match the front door contract")
		f.storeUnavailable(err)
		return err
	}
	f.state.Store(&snapshot{Ready: true, Info: info, CheckedAt: time.Now().UTC()})
	return nil
}

func (f *FrontDoor) probeReady(ctx context.Context) error {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.backend.ResolveReference(&url.URL{Path: "/readyz"}).String(), nil)
	response, err := f.probeClient.Do(request)
	if err != nil {
		return errors.New("backend readiness probe failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return errors.New("backend is not ready")
	}
	return nil
}

func (f *FrontDoor) probeVersion(ctx context.Context) (runtimeInfo, error) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.backend.ResolveReference(&url.URL{Path: "/version"}).String(), nil)
	response, err := f.probeClient.Do(request)
	if err != nil {
		return runtimeInfo{}, errors.New("backend version probe failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return runtimeInfo{}, errors.New("backend version probe was not successful")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxVersionBody))
	decoder.DisallowUnknownFields()
	var info runtimeInfo
	if err := decoder.Decode(&info); err != nil {
		return runtimeInfo{}, errors.New("backend version response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return runtimeInfo{}, errors.New("backend version response has trailing data")
	}
	return info, nil
}

func (f *FrontDoor) storeUnavailable(err error) {
	reason := "backend_unavailable"
	if err != nil && strings.Contains(err.Error(), "compatibility") {
		reason = "backend_incompatible"
	}
	previous := f.state.Load()
	info := runtimeInfo{}
	if previous != nil {
		info = previous.Info
	}
	f.state.Store(&snapshot{Info: info, CheckedAt: time.Now().UTC(), Reason: reason})
}

// Run probes immediately and then continuously until context cancellation.
func (f *FrontDoor) Run(ctx context.Context) {
	_ = f.Probe(ctx)
	ticker := time.NewTicker(f.probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = f.Probe(ctx)
		}
	}
}

func (f *FrontDoor) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
		switch r.URL.Path {
		case "/front-door/healthz":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok mcp-front-door "+normalizedCommit(f.frontDoorCommit)+"\n")
			return
		case "/front-door/readyz":
			state := f.state.Load()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if state == nil || !state.Ready {
				http.Error(w, "backend unavailable", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ready\n")
			return
		case "/front-door/version":
			f.writeFrontDoorVersion(w)
			return
		}
		state := f.state.Load()
		if state == nil || !state.Ready {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "MCP backend is temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		f.activeRequests.Add(1)
		defer f.activeRequests.Add(-1)
		f.proxy.ServeHTTP(w, r)
	})
}

func (f *FrontDoor) writeFrontDoorVersion(w http.ResponseWriter) {
	state := f.state.Load()
	response := struct {
		Status         string      `json:"status"`
		Commit         string      `json:"commit"`
		ActiveRequests int64       `json:"active_requests"`
		BackendReady   bool        `json:"backend_ready"`
		Backend        runtimeInfo `json:"backend,omitempty"`
		CheckedAt      time.Time   `json:"checked_at,omitempty"`
		Reason         string      `json:"reason,omitempty"`
	}{Status: "ok", Commit: normalizedCommit(f.frontDoorCommit), ActiveRequests: f.activeRequests.Load()}
	if state != nil {
		response.BackendReady = state.Ready
		response.Backend = state.Info
		response.CheckedAt = state.CheckedAt
		response.Reason = state.Reason
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (f *FrontDoor) validateResponse(response *http.Response) error {
	if response.Request.URL.Path == "/mcp" {
		if response.Header.Get("X-MCP-Catalog-Hash") != f.expectedCatalogHash {
			_ = response.Body.Close()
			f.storeUnavailable(errors.New("backend compatibility response mismatch"))
			return errors.New("backend MCP catalog identity changed")
		}
	}
	response.Header.Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
	return nil
}

func (f *FrontDoor) proxyError(w http.ResponseWriter, _ *http.Request, _ error) {
	f.storeUnavailable(errors.New("backend proxy failed"))
	w.Header().Set("Retry-After", "1")
	w.Header().Set("X-MCP-Front-Door-Commit", normalizedCommit(f.frontDoorCommit))
	http.Error(w, "MCP backend is temporarily unavailable", http.StatusServiceUnavailable)
}

func normalizedCommit(commit string) string {
	if commitPattern.MatchString(commit) {
		return commit
	}
	return "unknown"
}

func (f *FrontDoor) String() string {
	return fmt.Sprintf("front-door backend=%s protocol=%s catalog=%s", f.backend.Redacted(), f.expectedProtocol, f.expectedCatalogHash)
}
