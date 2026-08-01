package frontdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFrontDoorAdmissionReprobesAfterRequestArrival(t *testing.T) {
	var ready atomic.Bool
	var posts atomic.Int64
	ready.Store(true)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if !ready.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			posts.Add(1)
			http.Error(w, "escaped stale readiness", http.StatusBadGateway)
		}
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		AdmissionTimeout: 250 * time.Millisecond, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready.Store(false)
	request := httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	response := httptest.NewRecorder()
	door.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if posts.Load() != 0 {
		t.Fatal("request escaped readiness captured before admission")
	}
}
