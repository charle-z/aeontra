package frontdoor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFrontDoorAdmissionTimeoutIsBoundedAndDoesNotForward(t *testing.T) {
	var posts atomic.Int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			posts.Add(1)
		}
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer backend.Close()
	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		AdmissionTimeout: 250 * time.Millisecond, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = door.Probe(context.Background())
	request := httptest.NewRequest(http.MethodPost, "https://front.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	response := httptest.NewRecorder()
	started := time.Now()
	door.Handler().ServeHTTP(response, request)
	elapsed := time.Since(started)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if elapsed < 200*time.Millisecond || elapsed > time.Second {
		t.Fatalf("admission elapsed=%s", elapsed)
	}
	if posts.Load() != 0 {
		t.Fatal("timed-out request reached the backend")
	}
	if door.admissionTimeouts.Load() != 1 || door.admissionWaits.Load() != 1 {
		t.Fatalf("waits=%d timeouts=%d", door.admissionWaits.Load(), door.admissionTimeouts.Load())
	}
}
