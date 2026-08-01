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

func TestFrontDoorSSEHeaderWaitIsBounded(t *testing.T) {
	var streams atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			streams.Add(1)
			<-r.Context().Done()
		}
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		ProbeTimeout: 250 * time.Millisecond, AdmissionTimeout: 500 * time.Millisecond, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://front.example/mcp", nil)
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	started := time.Now()
	door.Handler().ServeHTTP(response, request)
	elapsed := time.Since(started)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if elapsed < 450*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("header wait elapsed=%s", elapsed)
	}
	if streams.Load() < 1 || door.activeRequests.Load() != 0 {
		t.Fatalf("streams=%d active=%d", streams.Load(), door.activeRequests.Load())
	}
}
