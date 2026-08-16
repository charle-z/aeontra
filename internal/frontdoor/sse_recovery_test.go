package frontdoor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type closedSSEWriter struct {
	header http.Header
	status int
}

func (w *closedSSEWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *closedSSEWriter) WriteHeader(status int)    { w.status = status }
func (w *closedSSEWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (w *closedSSEWriter) Flush()                    {}

func TestFrontDoorRecoversCommentOnlySSEWithoutReusingAuthorization(t *testing.T) {
	var streamRequests atomic.Int64
	var ready atomic.Bool
	ready.Store(true)
	retire := make(chan struct{})
	var retireOnce sync.Once
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			if !ready.Load() {
				http.Error(w, "replacement gap", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/version":
			if !ready.Load() {
				http.Error(w, "replacement gap", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "version": "0.2.0", "protocol_version": "2024-11-05",
				"commit": strings.Repeat("a", 40), "tool_count": 1, "catalog_hash": testCatalogHash,
			})
		case "/mcp":
			if streamRequests.Add(1) != 1 {
				// Models an access token that expired while the original downstream
				// connection remained open. Reusing it is both unnecessary and wrong.
				http.Error(w, "expired bearer", http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer short-lived" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(": mcp-devbox stream open\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-retire
		}
	}))
	defer backend.Close()
	defer retireOnce.Do(func() { close(retire) })

	door, err := New(Config{
		BackendURL: backend.URL, ExpectedProtocol: "2024-11-05", ExpectedCatalogHash: testCatalogHash,
		ProbeInterval: 250 * time.Millisecond, ProbeTimeout: time.Second,
		AdmissionTimeout: time.Second, Client: backend.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go door.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for {
		state := door.state.Load()
		if state != nil && state.Ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("front door did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	endpoint := httptest.NewServer(door.Handler())
	defer endpoint.Close()
	request, err := http.NewRequest(http.MethodGet, endpoint.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Authorization", "Bearer short-lived")
	request.Header.Set("Mcp-Session-Id", "durable-session")
	response, err := endpoint.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for _, want := range []string{": mcp-devbox stream open\n", "\n"} {
		if line, err := reader.ReadString('\n'); err != nil || line != want {
			t.Fatalf("initial line=%q want=%q err=%v", line, want, err)
		}
	}

	retireOnce.Do(func() { close(retire) })
	readDone := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadString('\n')
		readDone <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()
	select {
	case result := <-readDone:
		if result.err != nil || result.line != ": front-door stream recovered\n" {
			t.Fatalf("recovery line=%q err=%v", result.line, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("front door did not recover the accepted SSE stream")
	}
	if line, err := reader.ReadString('\n'); err != nil || line != "\n" {
		t.Fatalf("recovery separator=%q err=%v", line, err)
	}
	if streamRequests.Load() != 1 {
		t.Fatalf("backend SSE authorization was replayed %d times", streamRequests.Load())
	}
	if door.sseReconnects.Load() != 1 || door.sseReconnectFails.Load() != 0 {
		t.Fatalf("recoveries=%d failures=%d", door.sseReconnects.Load(), door.sseReconnectFails.Load())
	}

	// A second backend gap is observed entirely through exact readiness/catalog
	// probes. The accepted downstream stream remains authorized and connected.
	ready.Store(false)
	if err := door.Probe(context.Background()); err == nil {
		t.Fatal("second backend gap was accepted")
	}
	ready.Store(true)
	if err := door.Probe(context.Background()); err != nil {
		t.Fatalf("second replacement did not become compatible: %v", err)
	}
	readDone = make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadString('\n')
		readDone <- struct {
			line string
			err  error
		}{line: line, err: err}
	}()
	select {
	case result := <-readDone:
		if result.err != nil || result.line != ": front-door stream recovered\n" {
			t.Fatalf("second recovery line=%q err=%v", result.line, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("front door did not preserve SSE through the second replacement")
	}
	if streamRequests.Load() != 1 || door.sseReconnects.Load() != 2 || door.sseReconnectFails.Load() != 0 {
		t.Fatalf("stream requests=%d recoveries=%d failures=%d", streamRequests.Load(), door.sseReconnects.Load(), door.sseReconnectFails.Load())
	}
}

func TestFrontDoorDownstreamDisconnectIsNotBackendReconnectFailure(t *testing.T) {
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
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-MCP-Catalog-Hash", testCatalogHash)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(": mcp-devbox stream open\n\n"))
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
	if err := door.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://front.example/mcp", nil)
	request.Header.Set("Accept", "text/event-stream")
	writer := &closedSSEWriter{}
	door.Handler().ServeHTTP(writer, request)
	if writer.status != http.StatusOK {
		t.Fatalf("status=%d", writer.status)
	}
	if door.sseReconnectFails.Load() != 0 {
		t.Fatalf("downstream disconnect counted as backend failure: %d", door.sseReconnectFails.Load())
	}
	state := door.state.Load()
	if state == nil || !state.Ready {
		t.Fatal("downstream disconnect changed backend readiness")
	}
}
