package frontdoor

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFrontDoorSSEWaitsBeforeHeadersAndClientCancellationIsNotFailure(t *testing.T) {
	var ready atomic.Bool
	retire := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if !ready.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/version":
			if !ready.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			if !ready.Load() {
				http.Error(w, "no upstream", http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(": mcp-devbox stream open\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-retire:
			case <-r.Context().Done():
			}
		}
	}))
	defer backend.Close()

	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		AdmissionTimeout: time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = door.Probe(context.Background())
	endpoint := httptest.NewServer(door.Handler())
	defer endpoint.Close()

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	result := make(chan *http.Response, 1)
	errs := make(chan error, 1)
	go func() {
		response, err := endpoint.Client().Do(request)
		if err != nil {
			errs <- err
			return
		}
		result <- response
	}()

	select {
	case response := <-result:
		_ = response.Body.Close()
		t.Fatal("SSE returned headers while no compatible backend existed")
	case err := <-errs:
		t.Fatalf("SSE failed while waiting for backend: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	ready.Store(true)
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	var response *http.Response
	select {
	case response = <-result:
	case err := <-errs:
		t.Fatalf("SSE did not recover: %v", err)
	case <-time.After(time.Second):
		t.Fatal("SSE did not open after compatible backend returned")
	}
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != ": mcp-devbox stream open\n" {
		t.Fatalf("open line=%q err=%v", line, err)
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "\n" {
		t.Fatalf("separator=%q err=%v", line, err)
	}

	ready.Store(false)
	close(retire)
	deadline := time.Now().Add(time.Second)
	for {
		state := door.state.Load()
		if state != nil && !state.Ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("front door did not enter reconnect wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	_ = response.Body.Close()
	deadline = time.Now().Add(time.Second)
	for door.activeRequests.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if door.activeRequests.Load() != 0 {
		t.Fatal("cancelled SSE handler remained active")
	}
	if door.sseReconnectFails.Load() != 0 {
		t.Fatalf("client cancellation counted as reconnect failure: %d", door.sseReconnectFails.Load())
	}
	if door.admissionWaits.Load() < 2 || door.admissionRecoveries.Load() < 1 {
		t.Fatalf("waits=%d recoveries=%d", door.admissionWaits.Load(), door.admissionRecoveries.Load())
	}
}
