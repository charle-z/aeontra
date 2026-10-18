package frontdoor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestProbeDoesNotFollowBackendRedirects(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int64
	redirectTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "protocol_version": "2024-11-05",
			"commit":     "0123456789abcdef0123456789abcdef01234567",
			"tool_count": 106, "catalog_hash": testCatalogHash,
		})
	}))
	defer redirectTarget.Close()

	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			w.WriteHeader(http.StatusOK)
		case "/version":
			http.Redirect(w, r, redirectTarget.URL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05",
		ExpectedCatalogHash: testCatalogHash, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("redirected compatibility probe accepted")
	}
	if redirected.Load() != 0 {
		t.Fatalf("probe escaped fixed backend origin: requests=%d", redirected.Load())
	}
}
