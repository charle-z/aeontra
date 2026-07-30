package frontdoor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testCatalogHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestFrontDoorRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	for _, cfg := range []Config{
		{},
		{BackendURL: "ftp://backend.example", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash},
		{BackendURL: "https://user:pass@backend.example", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash},
		{BackendURL: "https://backend.example/path", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash},
		{BackendURL: "http://backend.example", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash},
		{BackendURL: "https://backend.example", ExpectedProtocol: "invalid", ExpectedCatalogHash: testCatalogHash},
		{BackendURL: "https://backend.example", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: "sha256:invalid"},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("unsafe config accepted: %+v", cfg)
		}
	}
}

func TestFrontDoorProxiesMCPHeadersAndFailsClosedOnIncompatibleBackend(t *testing.T) {
	t.Parallel()
	var compatible atomic.Bool
	compatible.Store(true)
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if !compatible.Load() {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ready\n")
		case "/version":
			hash := testCatalogHash
			if !compatible.Load() {
				hash = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": "0123456789abcdef0123456789abcdef01234567", "tool_count": 106, "catalog_hash": hash,
			})
		case "/mcp":
			if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Mcp-Session-Id") != "session-1" {
				http.Error(w, "missing forwarded identity", http.StatusBadRequest)
				return
			}
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		Client: backend.Client(), FrontDoorCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Mcp-Session-Id", "session-1")
	response := httptest.NewRecorder()
	door.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Mcp-Session-Id") != "session-1" {
		t.Fatalf("response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if response.Header().Get("X-MCP-Front-Door-Commit") != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("front door identity missing: %v", response.Header())
	}

	compatible.Store(false)
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("incompatible backend accepted")
	}
	response = httptest.NewRecorder()
	door.Handler().ServeHTTP(response, request.Clone(context.Background()))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("response=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestFrontDoorKeepsAcceptedRequestDuringBackendDrain(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var ready atomic.Bool
	ready.Store(true)
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if !ready.Load() {
				http.Error(w, "draining", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "protocol_version": "2024-11-05", "commit": "0123456789abcdef0123456789abcdef01234567",
				"tool_count": 106, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			close(started)
			<-release
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		}
	}))
	defer backend.Close()

	door, err := New(Config{BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash, Client: backend.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
		done <- response
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("backend request did not start")
	}
	ready.Store(false)
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("draining backend remained ready")
	}
	close(release)
	select {
	case response := <-done:
		if response.Code != http.StatusOK {
			t.Fatalf("accepted request was aborted: %d %s", response.Code, response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accepted request did not drain")
	}
}

func TestFrontDoorOwnHealthIsIndependentFromBackendReadiness(t *testing.T) {
	t.Parallel()
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer backend.Close()
	door, err := New(Config{BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash, Client: backend.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]int{"/front-door/healthz": http.StatusOK, "/front-door/readyz": http.StatusServiceUnavailable} {
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != expected {
			t.Fatalf("%s=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
