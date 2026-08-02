package frontdoor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const transitionCatalogHash = "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"

func TestFrontDoorAcceptsOnlyTwoExplicitCatalogs(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	activeCatalog := testCatalogHash
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		catalog := activeCatalog
		mu.Unlock()
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "protocol_version": "2024-11-05",
				"commit":     "0123456789abcdef0123456789abcdef01234567",
				"tool_count": 115, "catalog_hash": catalog,
			})
		case "/mcp":
			w.Header().Set("X-MCP-Catalog-Hash", catalog)
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, ": keepalive\n\n")
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05",
		ExpectedCatalogHash: testCatalogHash, TransitionCatalogHashes: []string{transitionCatalogHash},
		Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []string{testCatalogHash, transitionCatalogHash} {
		mu.Lock()
		activeCatalog = catalog
		mu.Unlock()
		if err := door.Probe(context.Background()); err != nil {
			t.Fatalf("approved catalog %s rejected: %v", catalog, err)
		}
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("approved catalog %s proxy response=%d body=%s", catalog, response.Code, response.Body.String())
		}
		stream, transient, err := door.openMCPStream(context.Background(), httptest.NewRequest(http.MethodGet, "https://front.example/mcp", nil))
		if err != nil || transient || stream.StatusCode != http.StatusOK {
			t.Fatalf("approved catalog %s SSE response=%v transient=%t err=%v", catalog, stream, transient, err)
		}
		_ = stream.Body.Close()
	}

	mu.Lock()
	activeCatalog = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	mu.Unlock()
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("third catalog accepted")
	}
}

func TestFrontDoorRejectsInvalidCatalogTransitionConfiguration(t *testing.T) {
	t.Parallel()
	base := Config{BackendURL: "https://backend.example", ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash}
	for _, transitions := range [][]string{
		{""},
		{"sha256:bad"},
		{testCatalogHash},
		{transitionCatalogHash, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	} {
		cfg := base
		cfg.TransitionCatalogHashes = transitions
		if _, err := New(cfg); err == nil {
			t.Fatalf("invalid transition catalogs accepted: %#v", transitions)
		}
	}
}

func TestFrontDoorKeepsOAuthDiscoveryAndDCRIndependentFromCatalogAdmission(t *testing.T) {
	t.Parallel()
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "protocol_version": "2024-11-05",
				"commit": "0123456789abcdef0123456789abcdef01234567", "tool_count": 999,
				"catalog_hash": "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			})
		case "/.well-known/oauth-protected-resource", "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"issuer":"https://front.example"}`)
		case "/oauth/register":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"client_id":"opaque"}`)
		case "/mcp":
			t.Fatal("incompatible MCP request reached backend")
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05",
		ExpectedCatalogHash: testCatalogHash, Client: backend.Client(), AdmissionTimeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("unknown catalog unexpectedly admitted")
	}
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "https://front.example/.well-known/oauth-protected-resource", nil),
		httptest.NewRequest(http.MethodGet, "https://front.example/.well-known/oauth-authorization-server", nil),
		httptest.NewRequest(http.MethodPost, "https://front.example/oauth/register", bytes.NewBufferString(`{"redirect_uris":["https://example.test/callback"]}`)),
	} {
		response := httptest.NewRecorder()
		door.Handler().ServeHTTP(response, request)
		if response.Code < 200 || response.Code >= 300 || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s -> %d %q", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	door.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://front.example/mcp", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("incompatible MCP response=%d body=%s", response.Code, response.Body.String())
	}
}
